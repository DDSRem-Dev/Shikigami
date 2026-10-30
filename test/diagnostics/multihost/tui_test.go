package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type tuiInput chan string

func (input tuiInput) Write(data []byte) (int, error) { input <- string(data); return len(data), nil }

func TestDashboardWaitsForReadyEveryPanelAndRequestedContent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	input := make(tuiInput, 16)
	changed, exited := make(chan struct{}, 1), make(chan struct{})
	var mu sync.Mutex
	var output strings.Builder
	appendOutput := func(value string) {
		mu.Lock()
		output.WriteString(value)
		mu.Unlock()
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	snapshot := func() (string, bool) { mu.Lock(); defer mu.Unlock(); return output.String(), false }
	wait := func(since int, want ...string) (int, error) {
		return waitTUIOutput(ctx, exited, changed, snapshot, since, want)
	}
	done := make(chan error, 1)
	go func() { done <- driveDashboard(input, wait, 6, []string{"EXPECTED-CONFLICT"}) }()
	assertNoKey := func() {
		t.Helper()
		select {
		case key := <-input:
			t.Fatalf("premature key %q", key)
		case <-time.After(20 * time.Millisecond):
		}
	}
	expectKey := func(want string) {
		t.Helper()
		select {
		case key := <-input:
			if key != want {
				t.Fatalf("got key %q, want %q", key, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	appendOutput("terminal negotiation")
	assertNoKey()
	appendOutput("[Sessions]")
	assertNoKey() // A partial screen without the keyboard footer is not ready.
	appendOutput("q quit")
	for _, panel := range []string{"[Sync & conflicts]", "[Ports]", "[Plugins]", "[Node]", "[Sessions]", "[Sync & conflicts]"} {
		expectKey("\t")
		assertNoKey() // Prior visits must not satisfy a repeated panel transition.
		appendOutput(panel)
	}
	assertNoKey() // All tabs rendered, but required content has not arrived.
	appendOutput("EXPECTED-CONFLICT")
	expectKey("q")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDashboardMissingOutputIsBoundedAndDoesNotQuit(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	input := make(tuiInput, 1)
	changed, exited := make(chan struct{}), make(chan struct{})
	wait := func(since int, want ...string) (int, error) {
		return waitTUIOutput(ctx, exited, changed, func() (string, bool) { return "", false }, since, want)
	}
	cancel()
	if err := driveDashboard(input, wait, 1, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	select {
	case key := <-input:
		t.Fatalf("sent key without UI readiness: %q", key)
	default:
	}
	close(exited)
	_, err := waitTUIOutput(t.Context(), exited, changed, func() (string, bool) { return "partial", false }, 0, []string{"q quit"})
	if err == nil {
		t.Fatal("accepted exited dashboard without expected output")
	}
}
