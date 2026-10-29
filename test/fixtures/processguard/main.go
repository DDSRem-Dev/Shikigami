// Command processguard verifies recorded process identities before scoped cleanup.
// It is acceptance tooling, not a shipped host or plugin.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

type identity struct {
	Role       string `json:"role"`
	PID        int    `json:"pid"`
	Created    int64  `json:"created_ms"`
	Executable string `json:"executable"`
}
type observation struct {
	Identity identity `json:"identity"`
	State    string   `json:"state"`
	Error    string   `json:"error,omitempty"`
	Signaled bool     `json:"signaled"`
}
type liveIdentity struct {
	created    int64
	executable string
	uids       []uint32
}

func matches(want identity, actual liveIdentity, uid uint32) bool {
	return want.PID > 1 && want.Created > 0 && filepath.IsAbs(want.Executable) &&
		want.Created == actual.created && want.Executable == actual.executable &&
		len(actual.uids) > 0 && slices.Contains(actual.uids, uid)
}
func observe(ctx context.Context, want identity, sig syscall.Signal) observation {
	result := observation{Identity: want}
	if want.PID <= 1 || want.Created <= 0 || !filepath.IsAbs(want.Executable) {
		result.State, result.Error = "invalid", "complete process identity required"
		return result
	}
	exists, err := process.PidExistsWithContext(ctx, int32(want.PID))
	if err != nil {
		result.State, result.Error = "unknown", err.Error()
		return result
	}
	if !exists {
		result.State = "absent"
		return result
	}
	p, err := process.NewProcessWithContext(ctx, int32(want.PID))
	if err != nil {
		result.State, result.Error = "unknown", err.Error()
		return result
	}
	created, err := p.CreateTimeWithContext(ctx)
	if err != nil {
		result.State, result.Error = "unknown", err.Error()
		return result
	}
	executable, err := p.ExeWithContext(ctx)
	if err != nil {
		result.State, result.Error = "unknown", err.Error()
		return result
	}
	uids, err := p.UidsWithContext(ctx)
	if err != nil {
		result.State, result.Error = "unknown", err.Error()
		return result
	}
	if !matches(want, liveIdentity{created, executable, uids}, uint32(os.Getuid())) {
		result.State = "identity-mismatch"
		return result
	}
	result.State = "present"
	if sig != 0 {
		// A freshly allocated Process avoids gopsutil's cached creation time.
		recheck, err := process.NewProcessWithContext(ctx, int32(want.PID))
		if err != nil {
			result.State, result.Error = "unknown", err.Error()
			return result
		}
		current, err := recheck.CreateTimeWithContext(ctx)
		if err != nil || current != want.Created {
			result.State = "identity-mismatch"
			return result
		}
		if err := recheck.SendSignalWithContext(ctx, sig); err != nil {
			result.State, result.Error = "unknown", err.Error()
			return result
		}
		result.Signaled = true
	}
	return result
}
func run() error {
	inventory := flag.String("inventory", "", "private process inventory JSON")
	signalName := flag.String("signal", "", "optional TERM or KILL after exact identity verification")
	flag.Parse()
	if *inventory == "" || flag.NArg() != 0 {
		return fmt.Errorf("inventory is required")
	}
	var sig syscall.Signal
	switch *signalName {
	case "":
	case "TERM":
		sig = syscall.SIGTERM
	case "KILL":
		sig = syscall.SIGKILL
	default:
		return fmt.Errorf("only TERM or KILL is supported")
	}
	file, err := os.Open(*inventory)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return fmt.Errorf("invalid process inventory")
	}
	var input struct {
		Processes []identity `json:"processes"`
	}
	if err := json.NewDecoder(file).Decode(&input); err != nil {
		return err
	}
	if len(input.Processes) == 0 || len(input.Processes) > 128 {
		return fmt.Errorf("process count must be 1..128")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results := make([]observation, 0, len(input.Processes))
	for _, want := range input.Processes {
		results = append(results, observe(ctx, want, sig))
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"processes": results})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
