//go:build linux

package helper

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Only the test executable and inherited pipes are used. No installed helper,
// sudo, native resolver, privileged path or external network is touched.
func TestRegionCommandFixture(t *testing.T) {
	mode := os.Getenv("SHIKI_REGION_COMMAND_FIXTURE")
	if mode == "" {
		return
	}
	ready := os.NewFile(3, "owned-ready")
	if mode == "child" {
		if _, err := fmt.Fprintln(ready, os.Getpid()); err != nil {
			os.Exit(2)
		}
		ready.Close()
		time.Sleep(time.Minute)
		os.Exit(3)
	}
	executable, err := os.Executable()
	if err != nil {
		os.Exit(4)
	}
	child := exec.Command(executable, "-test.run=^TestRegionCommandFixture$")
	child.Env = []string{"SHIKI_REGION_COMMAND_FIXTURE=child", "GOMAXPROCS=2"}
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	child.ExtraFiles = []*os.File{ready}
	if mode == "escaped" {
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := child.Start(); err != nil {
		os.Exit(5)
	}
	ready.Close()
	release := os.NewFile(4, "owned-release")
	var next [1]byte
	if _, err := release.Read(next[:]); err != nil {
		os.Exit(6)
	}
	if _, err := io.WriteString(os.Stdout, `{"code":"fixture"}`); err != nil {
		os.Exit(7)
	}
	os.Exit(0) // intentionally leave the descendant holding both output pipes
}

type ownedCommandFixture struct {
	command *exec.Cmd
	ready   *os.File
	release *os.File
}

func newOwnedCommandFixture(t *testing.T, mode string) *ownedCommandFixture {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready, readyWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	releaseReader, release, err := os.Pipe()
	if err != nil {
		ready.Close()
		readyWriter.Close()
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestRegionCommandFixture$")
	command.Env = []string{"SHIKI_REGION_COMMAND_FIXTURE=" + mode, "GOMAXPROCS=2"}
	command.ExtraFiles = []*os.File{readyWriter, releaseReader}
	t.Cleanup(func() {
		ready.Close()
		readyWriter.Close()
		releaseReader.Close()
		release.Close()
	})
	return &ownedCommandFixture{command: command, ready: ready, release: release}
}

func (f *ownedCommandFixture) childHandle(t *testing.T) int {
	t.Helper()
	if err := f.ready.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(f.ready).ReadString('\n')
	if err != nil {
		t.Fatalf("owned child did not become ready: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid owned child readiness: %q, %v", line, err)
	}
	// The fixture remains alive behind the release pipe while its stable
	// pidfd is opened. Cleanup never signals a PID read from stale state.
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
		_ = unix.Close(fd)
	})
	return fd
}

func assertOwnedChildExited(t *testing.T, fd int) {
	t.Helper()
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	deadline := time.Now().Add(2 * time.Second)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatal("owned descendant survived command cleanup deadline")
		}
		count, err := unix.Poll(poll, int((remaining+time.Millisecond-1)/time.Millisecond))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || count != 1 || poll[0].Revents&unix.POLLIN == 0 {
			t.Fatalf("owned descendant survived command cleanup: count=%d events=%d err=%v", count, poll[0].Revents, err)
		}
		return
	}
}

func awaitCommandResult(t *testing.T, completed <-chan error) error {
	t.Helper()
	select {
	case err := <-completed:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("owned command retained output pipes beyond its bounded drain")
		return nil
	}
}

func TestOwnedCommandReapsGroupBeforeReturning(t *testing.T) {
	for _, cancelCommand := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelCommand), func(t *testing.T) {
			fixture := newOwnedCommandFixture(t, "leader")
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			stdout, stderr := &limitedBuffer{maximum: 4096}, &limitedBuffer{maximum: 4096}
			fixture.command.Stdout, fixture.command.Stderr = stdout, stderr
			completed := make(chan error, 1)
			go func() { completed <- runOwnedCommand(ctx, fixture.command) }()
			child := fixture.childHandle(t)
			if cancelCommand {
				cancel()
			} else if _, err := fixture.release.Write([]byte{1}); err != nil {
				t.Fatal(err)
			}
			err := awaitCommandResult(t, completed)
			if cancelCommand {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled command lost its cancellation: %v", err)
				}
			} else if err != nil || string(stdout.Bytes()) != `{"code":"fixture"}` {
				t.Fatalf("successful command did not drain after descendant cleanup: %q, %v", stdout.Bytes(), err)
			}
			assertOwnedChildExited(t, child)
			if fixture.command.ProcessState == nil {
				t.Fatal("owned leader was not reaped")
			}
		})
	}
}

func TestOwnedCommandBoundsEscapedOutputHolderWithoutKillingOtherGroup(t *testing.T) {
	fixture := newOwnedCommandFixture(t, "escaped")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	fixture.command.Stdout = &limitedBuffer{maximum: 4096}
	fixture.command.Stderr = &limitedBuffer{maximum: 4096}
	completed := make(chan error, 1)
	go func() { completed <- runOwnedCommand(ctx, fixture.command) }()
	child := fixture.childHandle(t)
	if _, err := fixture.release.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := awaitCommandResult(t, completed); !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("inherited output pipes were not explicitly bounded: %v", err)
	}
	poll := []unix.PollFd{{Fd: int32(child), Events: unix.POLLIN}}
	if count, err := unix.Poll(poll, 0); err != nil || count != 0 {
		t.Fatalf("command cleanup crossed its owned process-group boundary: count=%d, err=%v", count, err)
	}
	if err := unix.PidfdSendSignal(child, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	assertOwnedChildExited(t, child)
}

func TestHelperClientCancellationClosesItsCallAndOwnedGroup(t *testing.T) {
	fixture := newOwnedCommandFixture(t, "leader")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	completed := make(chan error, 1)
	go func() {
		_, err := callHelperCommand(ctx, fixture.command, []byte(`{}`))
		completed <- err
	}()
	child := fixture.childHandle(t)
	cancel()
	if err := awaitCommandResult(t, completed); !errors.Is(err, context.Canceled) {
		t.Fatalf("helper cancellation did not promptly terminate the client call: %v", err)
	}
	assertOwnedChildExited(t, child)
}

func TestNativeCommandCancellationReleasesWriterAndRetainsRecovery(t *testing.T) {
	f := newFixture(t)
	fixture := newOwnedCommandFixture(t, "leader")
	fixture.command.Stdout = &limitedBuffer{maximum: 65536}
	fixture.command.Stderr = &limitedBuffer{maximum: 4096}
	before, err := f.backend.Read(context.Background(), "resolver")
	if err != nil {
		t.Fatal(err)
	}
	plan := f.plan(t, "native-cancel-plan", true)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	calls := 0
	f.backend.reloadFault = func(ctx context.Context, _ Transition) error {
		calls++
		if calls == 1 {
			return runOwnedCommand(ctx, fixture.command)
		}
		return errors.New("controlled recovery reload failure")
	}
	completed := make(chan Response, 1)
	go func() { completed <- f.engine.Handle(ctx, "apply", applyRequest(plan, "native-cancel-apply")) }()
	child := fixture.childHandle(t)
	cancel()
	var response Response
	select {
	case response = <-completed:
	case <-time.After(3 * time.Second):
		t.Fatal("native command retained the root writer lock after cancellation")
	}
	if response.Error != "" || response.Operation.State != "rollback_failed" {
		t.Fatalf("failed native command discarded pending recovery evidence: %+v", response)
	}
	assertOwnedChildExited(t, child)
	operationID := response.Operation.Id
	if err := NewFileStore(f.state, false).WithLock(context.Background(), func(j *Journal, _ func() error) error {
		if j.Owner != operationID || j.Pending != operationID || j.RecoveryProblem != operationID {
			t.Fatalf("native cancellation released durable recovery ownership: %+v", j)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.backend.reloadFault = nil
	f.restart()
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if operation := f.operation(t, operationID); operation.State != "rolled_back" {
		t.Fatalf("independent guard could not retry after bounded command failure: %v", operation)
	}
	after, err := f.backend.Read(context.Background(), "resolver")
	if err != nil || !bytes.Equal(before.Data, after.Data) {
		t.Fatalf("independent recovery did not restore the resolver: %q, %v", after.Data, err)
	}
}
