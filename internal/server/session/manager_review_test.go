package session

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func interceptTmux(t *testing.T, action, behavior string) (*tmux.Client, string) {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	t.Setenv("SHIKI_TEST_TMUX_REAL", real)
	t.Setenv("SHIKI_TEST_TMUX_ACTION", action)
	t.Setenv("SHIKI_TEST_TMUX_BEHAVIOR", behavior)
	t.Setenv("SHIKI_TEST_TMUX_READY", marker)
	script := `#!/bin/sh
for shiki_arg do
  if [ "$shiki_arg" = "$SHIKI_TEST_TMUX_ACTION" ]; then
    : > "$SHIKI_TEST_TMUX_READY"
    case "$SHIKI_TEST_TMUX_BEHAVIOR" in
      fail) exit 42 ;;
      block) exec /bin/sleep 30 ;;
    esac
  fi
done
exec "$SHIKI_TEST_TMUX_REAL" "$@"
`
	path := filepath.Join(dir, "tmux-wrapper")
	if err = os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := tmux.New(path)
	if err != nil {
		t.Fatal(err)
	}
	return client, marker
}

func TestMissingClaudeDoesNotCreateOrKillExistingProcess(t *testing.T) {
	m, _ := managerFixture(t, 10)
	original := append([]string(nil), m.opts.ClaudeCommand...)
	m.operations.Lock()
	m.opts.ClaudeCommand = []string{filepath.Join(t.TempDir(), "missing-claude")}
	m.operations.Unlock()
	request := &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "missing", RequestId: "missing"}
	if _, err := m.Create(t.Context(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("missing tool creation: %v", err)
	}
	state, _ := m.store.View()
	if len(state.Buckets[storage.SessionsBucket]) != 0 {
		t.Fatal("dependency failure reserved a session")
	}
	no := false
	shell, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "shell", RequestId: "shell", AutoStartClaude: &no})
	if err != nil {
		t.Fatal("shell-only sessions must work without Claude", err)
	}
	before, err := m.tmux.Inspect(t.Context(), shell.Id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.RestartClaude(t.Context(), &pb.SessionActionRequest{SessionId: shell.Id}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("missing tool restart: %v", err)
	}
	after, err := m.tmux.Inspect(t.Context(), shell.Id)
	if err != nil || after.PID != before.PID {
		t.Fatal("invalid restart killed the existing pane")
	}
	m.operations.Lock()
	m.opts.ClaudeCommand = original
	m.operations.Unlock()
	if _, err = m.Create(t.Context(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("original failed request silently retried after dependency repair", err)
	}
}

func TestSessionWorkingDirectoryMustBeUsableByDaemon(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission denial requires ordinary user")
	}
	m, root := managerFixture(t, 10)
	directory := filepath.Join(root, "read-only")
	if err := os.Mkdir(directory, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0700)
	no := false
	if _, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "unwritable", WorkingDir: directory, RequestId: "unwritable", AutoStartClaude: &no}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("unusable working directory admitted: %v", err)
	}
	state, _ := m.store.View()
	if len(state.Buckets[storage.SessionsBucket]) != 0 {
		t.Fatal("unusable directory created a session")
	}
}

func TestFailedCreationReleasesNameOnlyAfterConfirmedCleanup(t *testing.T) {
	m, _ := managerFixture(t, 1)
	real := m.tmux
	wrapper, _ := interceptTmux(t, "new-session", "fail")
	m.operations.Lock()
	m.tmux = wrapper
	m.operations.Unlock()
	request := &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "reusable", RequestId: "original-failure"}
	if _, err := m.Create(t.Context(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("unexpected create outcome: %v", err)
	}
	state, _ := m.store.View()
	if len(state.Buckets[storage.SessionsBucket]) != 0 {
		t.Fatal("confirmed cleanup retained name/quota reservation")
	}
	record := state.Requests[createNamespace+"\x00"+request.RequestId]
	if record.Status != "failed" {
		t.Fatal("failure result not retained")
	}
	m.operations.Lock()
	m.tmux = real
	m.operations.Unlock()
	if _, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "reusable", RequestId: "new-attempt"}); err != nil {
		t.Fatal("failed name was not released", err)
	}
	if _, err := m.Create(t.Context(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("old request lost stable failure", err)
	}
}

func TestFailedStopAndInterruptedRestartKeepHonestQuota(t *testing.T) {
	m, _ := managerFixture(t, 10)
	m.operations.Lock()
	m.opts.MaxClaude = 1
	m.operations.Unlock()
	s, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "live", RequestId: "live"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := m.tmux.Inspect(t.Context(), s.Id)
	if err != nil {
		t.Fatal(err)
	}
	real := m.tmux
	wrapper, _ := interceptTmux(t, "respawn-pane", "fail")
	m.operations.Lock()
	m.tmux = wrapper
	m.operations.Unlock()
	if _, err = m.StopClaude(t.Context(), &pb.SessionActionRequest{SessionId: s.Id}); err == nil {
		t.Fatal("failed stop reported success")
	}
	m.operations.Lock()
	m.tmux = real
	m.operations.Unlock()
	if _, err = m.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := m.Get(t.Context(), &pb.GetSessionRequest{Id: s.Id})
	if err != nil || current.ClaudeStatus != "unknown" {
		t.Fatalf("failed stop must remain unknown: %+v %v", current, err)
	}
	after, err := real.Inspect(t.Context(), s.Id)
	if err != nil || after.PID != before.PID {
		t.Fatal("failed stop unexpectedly replaced process")
	}
	if _, err = m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "over-quota", RequestId: "over-quota"}); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal("unknown live process did not consume quota", err)
	}
	if err = m.store.Update(func(state *storage.Snapshot) error {
		var value pb.Session
		if err := state.Get(storage.SessionsBucket, s.Id, &value); err != nil {
			return err
		}
		value.ClaudeStatus = "starting"
		return state.Put(storage.SessionsBucket, s.Id, &value)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err = m.Get(t.Context(), &pb.GetSessionRequest{Id: s.Id})
	if err != nil || current.ClaudeStatus != "unknown" {
		t.Fatal("interrupted restart was reported as confirmed running")
	}
	if _, err = m.StopClaude(t.Context(), &pb.SessionActionRequest{SessionId: s.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "after-stop", RequestId: "after-stop"}); err != nil {
		t.Fatal("confirmed stop did not release process quota", err)
	}
}

func TestShutdownCancelsInFlightCreateAndRejectsLaterMutations(t *testing.T) {
	m, _ := managerFixture(t, 10)
	wrapper, marker := interceptTmux(t, "new-session", "block")
	m.operations.Lock()
	m.tmux = wrapper
	m.operations.Unlock()
	created := make(chan error, 1)
	go func() {
		_, err := m.Create(context.Background(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "blocked", RequestId: "blocked"})
		created <- err
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("create did not reach tmux command")
		case <-time.After(5 * time.Millisecond):
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manager Close blocked behind live process operation")
	}
	select {
	case err := <-created:
		if connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatalf("shutdown creation error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("create leaked after shutdown")
	}
	if _, err := m.Delete(t.Context(), &pb.DeleteSessionRequest{Id: "sess_unknown"}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal("closed manager accepted mutation", err)
	}
	if _, err := m.Recover(t.Context()); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal("closed manager resumed recovery", err)
	}
}

func TestInterruptedCreatingShellIsNotClaimedAsRunningClaude(t *testing.T) {
	m, root := managerFixture(t, 10)
	id := "sess_" + rand.Text()
	request := &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "interrupted-shell", RequestId: "interrupted-shell"}
	if _, err := m.tmux.Create(t.Context(), id, root, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(state *storage.Snapshot) error {
		record, _, err := state.Request(createNamespace, request.RequestId, request)
		if err != nil {
			return err
		}
		record.ResourceID = id
		state.Requests[createNamespace+"\x00"+request.RequestId] = record
		return state.Put(storage.SessionsBucket, id, &pb.Session{Id: id, ProjectId: "proj_fixture", Name: request.Name, WorkingDir: root, CreatedAt: time.Now().Unix(), Lifecycle: "creating", ClaudeStatus: "starting"})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err := m.Create(t.Context(), request)
	if err != nil || result.Lifecycle != "ready" || result.ClaudeStatus != "unknown" {
		t.Fatalf("partial creation incorrectly claimed a running Claude: %+v %v", result, err)
	}
}

func TestOrphanWithMultiplePanesDoesNotBlockManagedRecovery(t *testing.T) {
	m, root := managerFixture(t, 10)
	id := "sess_" + rand.Text()
	pane, err := m.tmux.Create(t.Context(), id, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	socket := "shiki-" + strconv.Itoa(os.Getuid())
	t.Cleanup(func() { exec.Command(binary, "-L", socket, "kill-session", "-t", pane.TmuxSessionID).Run() })
	if output, err := exec.CommandContext(t.Context(), binary, "-L", socket, "split-window", "-d", "-t", pane.TmuxSessionID).CombinedOutput(); err != nil {
		t.Fatalf("split orphan: %s %v", output, err)
	}
	report, err := m.Recover(t.Context())
	if err != nil {
		t.Fatal("unclaimed orphan blocked managed recovery", err)
	}
	count := 0
	for _, orphan := range report.OrphanPanes {
		if orphan.SessionID == id {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("orphan report lost panes: %d", count)
	}
	if _, err := m.tmux.Inspect(t.Context(), id); errors.Is(err, tmux.ErrNotFound) {
		t.Fatal("recovery deleted orphan")
	}
}
