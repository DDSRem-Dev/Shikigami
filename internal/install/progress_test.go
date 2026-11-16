package install

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type progressRecorder struct {
	mu     sync.Mutex
	data   bytes.Buffer
	writes chan string
}

func (r *progressRecorder) Write(data []byte) (int, error) {
	r.mu.Lock()
	n, err := r.data.Write(data)
	r.mu.Unlock()
	r.writes <- string(data)
	return n, err
}

func (r *progressRecorder) snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data.String()
}

func awaitProgress(t *testing.T, r *progressRecorder, fragment string) {
	t.Helper()
	select {
	case output := <-r.writes:
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected %q in %q", fragment, output)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for progress %q", fragment)
	}
}

func TestProgressHeartbeatStopsBeforeNextPrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := &progressRecorder{writes: make(chan string, 10)}
	ticks := make(chan time.Time)
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- progressStep(ctx, recorder, "Installing system packages", ticks, func() error {
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	awaitProgress(t, recorder, "Installing system packages…")
	select {
	case ticks <- time.Now():
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat did not receive tick")
	}
	awaitProgress(t, recorder, "still working")
	close(release)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("progress did not join")
	}
	awaitProgress(t, recorder, ": done")
	// A subsequent prompt is written only after the reporter has joined. The
	// original tick channel must have no listener after the operation returns.
	_, _ = recorder.Write([]byte("Keyring password: "))
	select {
	case ticks <- time.Now():
		t.Fatal("heartbeat survived into the password prompt")
	default:
	}
	if !strings.HasSuffix(recorder.snapshot(), "Keyring password: ") {
		t.Fatal("progress output corrupted the next prompt")
	}
}

func TestProgressCancellationAndErrorNeverPrintSecret(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	recorder := &progressRecorder{writes: make(chan string, 10)}
	ticks := make(chan time.Time)
	finished := make(chan error, 1)
	want := errors.New("private-password-must-not-appear")
	go func() {
		finished <- progressStep(ctx, recorder, "Checking encrypted keyring", ticks, func() error {
			<-ctx.Done()
			return want
		})
	}()
	awaitProgress(t, recorder, "Checking encrypted keyring…")
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, want) {
			t.Fatalf("error identity changed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled progress did not join")
	}
	awaitProgress(t, recorder, ": failed")
	if strings.Contains(recorder.snapshot(), want.Error()) {
		t.Fatal("progress exposed an operation error containing secret data")
	}
	select {
	case ticks <- time.Now():
		t.Fatal("heartbeat survived canceled operation")
	default:
	}
}
