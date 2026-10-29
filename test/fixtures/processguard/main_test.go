package main

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

func TestSignalRequiresExactLiveIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, "sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	p, err := process.NewProcess(int32(child.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	created, err := p.CreateTime()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := p.Exe()
	if err != nil {
		t.Fatal(err)
	}
	want := identity{Role: "fixture", PID: child.Process.Pid, Created: created, Executable: executable}
	for _, mutate := range []func(*identity){
		func(v *identity) { v.Created++ },
		func(v *identity) { v.Executable += "-different" },
		func(v *identity) { v.Created = 0 },
	} {
		wrong := want
		mutate(&wrong)
		result := observe(ctx, wrong, 15)
		if result.Signaled || result.State == "present" {
			t.Fatalf("mismatched identity signaled: %+v", result)
		}
		if result := observe(ctx, want, 0); result.State != "present" {
			t.Fatalf("original child was affected: %+v", result)
		}
	}
	if result := observe(ctx, want, 15); !result.Signaled {
		t.Fatalf("owned child not signaled: %+v", result)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("signal should end sleep")
	}
	if result := observe(ctx, want, 0); result.State != "absent" {
		t.Fatalf("reaped process still present: %+v", result)
	}
}

func TestDifferentUIDCannotMatch(t *testing.T) {
	want := identity{Role: "fixture", PID: 234, Created: 123, Executable: "/owned/executable"}
	actual := liveIdentity{created: want.Created, executable: want.Executable, uids: []uint32{uint32(os.Getuid()) + 1}}
	if matches(want, actual, uint32(os.Getuid())) {
		t.Fatal("different UID matched")
	}
}
