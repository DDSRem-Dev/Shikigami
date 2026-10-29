//go:build e2e

package e2e

import (
	"context"
	"errors"
	processinfo "github.com/shirou/gopsutil/v4/process"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestDefaultRotationHeldTCPObserverJoinsEOFTimeoutAndCancellation(t *testing.T) {
	t.Run("fresh EOF timestamp and bounded tail", func(t *testing.T) {
		local, remote := net.Pipe()
		defer remote.Close()
		before := time.Now()
		observer := startRotationTCPObserver(local, before, before.Add(time.Second))
		defer observer.close()
		sender := make(chan struct{})
		go func() { defer close(sender); _, _ = remote.Write([]byte(rotationTail)); remote.Close() }()
		result := observer.wait(t.Context())
		<-sender
		if err := rotationTCPTerminationAllowed(result, before, before.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("deadline is failure not EOF", func(t *testing.T) {
		local, remote := net.Pipe()
		defer remote.Close()
		before := time.Now()
		observer := startRotationTCPObserver(local, before, before.Add(30*time.Millisecond))
		defer observer.close()
		result := observer.wait(t.Context())
		if result.Error == "" || rotationTCPTerminationAllowed(result, before, before.Add(time.Second)) == nil {
			t.Fatal("idle timeout was accepted as natural termination", result)
		}
	})
	t.Run("cancel joins reader", func(t *testing.T) {
		local, remote := net.Pipe()
		defer remote.Close()
		before := time.Now()
		observer := startRotationTCPObserver(local, before, before.Add(time.Hour))
		defer observer.close()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		result := observer.wait(ctx)
		if result.Error == "" {
			t.Fatal("cancellation was accepted")
		}
		select {
		case <-observer.done:
		default:
			t.Fatal("cancelled observer leaked reader")
		}
	})
}

func TestDefaultRotationProcessObserverOwnsAndJoinsSamples(t *testing.T) {
	command := exec.Command("sleep", "10")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			command.Process.Kill()
			command.Wait()
		}
	})
	p, err := processinfo.NewProcess(int32(command.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	created, err := p.CreateTimeWithContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	observer := startRotationProcessObserver(t.Context(), rotationProcessIdentity{PID: command.Process.Pid, Created: created})
	defer observer.close()
	time.Sleep(40 * time.Millisecond)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	select {
	case <-observer.done:
	case <-time.After(2 * time.Second):
		t.Fatal("observer did not terminate after owned process exit")
	}
	result := observer.result
	if result.LastPresent.IsZero() || result.FirstAbsent.IsZero() || result.LastPresent.After(result.FirstAbsent) || result.MaximumSamplingGap <= 0 {
		t.Fatal("invalid bounded sampling evidence", result)
	}
}

func TestDefaultRotationProcessObserverRejectsUnknownInspectionError(t *testing.T) {
	observer := startRotationProcessObserverWithProbe(t.Context(), rotationProcessIdentity{PID: 123, Created: 456}, func(context.Context, rotationProcessIdentity) (bool, error) { return false, os.ErrPermission })
	defer observer.close()
	select {
	case <-observer.done:
	case <-time.After(time.Second):
		t.Fatal("error observer did not join")
	}
	if observer.result.Error == "" || !observer.result.FirstAbsent.IsZero() || !observer.result.LastPresent.IsZero() {
		t.Fatal("permission error was accepted as a retirement interval", observer.result)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	canceled := startRotationProcessObserverWithProbe(ctx, rotationProcessIdentity{}, func(ctx context.Context, _ rotationProcessIdentity) (bool, error) { return false, ctx.Err() })
	canceled.close()
	if canceled.result.Error != "" || !canceled.result.FirstAbsent.IsZero() {
		t.Fatal("canceled observer fabricated retirement", canceled.result)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("test cancellation missing")
	}
}
