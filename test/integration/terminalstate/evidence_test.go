package terminalstate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

var scenarioContext context.Context

func TestMain(m *testing.M) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	scenarioContext = ctx
	code := m.Run()
	stop()
	os.Exit(code)
}

type processIdentity struct {
	Role       string `json:"role"`
	PID        int    `json:"pid"`
	Created    int64  `json:"created_ms"`
	Executable string `json:"executable"`
}

func (f *terminalFixture) track(t *testing.T) {
	t.Helper()
	if f.identities == nil {
		f.identities = map[int]processIdentity{}
	}
	parent, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	children, err := parent.ChildrenWithContext(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[int]string{f.pane.PID: "pane"}
	for _, p := range children {
		cmd, err := p.CmdlineWithContext(f.ctx)
		if err != nil {
			continue
		}
		if strings.Contains(cmd, "attach-session") && strings.Contains(cmd, f.pane.TmuxSessionID) {
			targets[int(p.Pid)] = "tmux-attach"
		}
	}
	for pid, role := range targets {
		if _, ok := f.identities[pid]; ok {
			continue
		}
		p, err := process.NewProcess(int32(pid))
		if err != nil {
			t.Fatal(err)
		}
		born, err := p.CreateTimeWithContext(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		exe, err := p.ExeWithContext(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		f.identities[pid] = processIdentity{Role: role, PID: pid, Created: born, Executable: exe}
	}
	f.writeEvidence(t, "processes.json", map[string]any{"processes": f.records()})
}
func (f *terminalFixture) records() []processIdentity {
	out := make([]processIdentity, 0, len(f.identities))
	for _, record := range f.identities {
		out = append(out, record)
	}
	return out
}
func (f *terminalFixture) writeEvidence(t *testing.T, name string, value any) {
	t.Helper()
	dir := os.Getenv("SHIKI_RUN_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func (f *terminalFixture) verifyCleanup(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, r := range f.identities {
		for {
			exists, err := process.PidExistsWithContext(ctx, int32(r.PID))
			if err != nil {
				t.Error(err)
				break
			}
			if !exists {
				break
			}
			select {
			case <-ctx.Done():
				t.Error("owned process remains", r)
			case <-time.After(20 * time.Millisecond):
				continue
			}
			break
		}
	}
	err := os.RemoveAll(f.root)
	if err != nil {
		t.Error(err)
	}
	_, stat := os.Stat(f.root)
	f.writeEvidence(t, "cleanup.json", map[string]any{"runtime": f.root, "runtime_absent": os.IsNotExist(stat), "processes": f.records(), "failed": t.Failed(), "remove_error": fmt.Sprint(err)})
}
