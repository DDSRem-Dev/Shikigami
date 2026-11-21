package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// Intentionally retain the supplied slice: Store.Get must not rely on custom
// decoders obeying encoding/json's usual copy-before-retaining convention.
type retainedJSON []byte

func (r *retainedJSON) UnmarshalJSON(data []byte) error { *r = data; return nil }

func TestStoreGetReturnsIndependentCompleteRecord(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "state"), Options{})
	exit := 7
	original := retryState{Reason: "live", Next: 123, Exit: &exit, Labels: map[string]string{"key": "value"}, Queue: []string{"queued"}}
	if err := s.Update(func(snapshot *Snapshot) error { return snapshot.Put("records", "one", original) }); err != nil {
		t.Fatal(err)
	}
	var first retryState
	if err := s.Get("records", "one", &first); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, original) {
		t.Fatal(first)
	}
	first.Labels["key"], first.Queue[0], *first.Exit = "modified", "modified", 99
	var raw retainedJSON
	if err := s.Get("records", "one", &raw); err != nil {
		t.Fatal(err)
	}
	clear(raw)
	var independent retryState
	if err := s.Get("records", "one", &independent); err != nil || !reflect.DeepEqual(independent, original) {
		t.Fatalf("caller mutated live record: %+v, %v", independent, err)
	}
	replacement := retryState{Reason: "revoked"}
	if err := s.Update(func(snapshot *Snapshot) error { return snapshot.Put("records", "one", replacement) }); err != nil {
		t.Fatal(err)
	}
	if err := s.Get("records", "one", &first); err != nil || !reflect.DeepEqual(first, replacement) {
		t.Fatalf("stale fields survived replacement: %+v, %v", first, err)
	}
	if independent.Reason != "live" || independent.Labels["key"] != "value" {
		t.Fatal("update changed earlier result")
	}
	if err := s.Update(func(snapshot *Snapshot) error { delete(snapshot.Buckets["records"], "one"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Get("records", "one", &first); !errors.Is(err, os.ErrNotExist) || !reflect.DeepEqual(first, replacement) {
		t.Fatalf("deleted record remained visible or changed target: %+v, %v", first, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Get("records", "one", &first); !errors.Is(err, ErrClosed) || !reflect.DeepEqual(first, replacement) {
		t.Fatalf("closed store read: %+v, %v", first, err)
	}
}

func TestStoreGetFailedDecodePreservesDestination(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "state"), Options{})
	if err := s.Update(func(snapshot *Snapshot) error {
		return snapshot.Put("records", "one", map[string]any{"reason": "new", "next": "wrong-type"})
	}); err != nil {
		t.Fatal(err)
	}
	target := retryState{Reason: "keep", Next: 19, Labels: map[string]string{"keep": "value"}}
	before := target
	if err := s.Get("records", "one", &target); err == nil || !reflect.DeepEqual(before, target) {
		t.Fatalf("failed decoder changed target: %+v, %v", target, err)
	}
	for _, invalid := range []any{nil, retryState{}, (*retryState)(nil)} {
		var want *json.InvalidUnmarshalError
		if err := s.Get("records", "one", invalid); !errors.As(err, &want) {
			t.Fatalf("invalid target error: %v", err)
		}
	}
}

func TestStoreGetConcurrentCommittedRecords(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "state"), Options{})
	type record struct {
		Generation int
		Label      string
		Revoked    bool
	}
	write := func(generation int) error {
		return s.Update(func(snapshot *Snapshot) error {
			return snapshot.Put("tokens", "one", record{Generation: generation, Label: fmt.Sprint(generation), Revoked: generation%2 == 1})
		})
	}
	if err := write(0); err != nil {
		t.Fatal(err)
	}
	var readers sync.WaitGroup
	stop := make(chan struct{})
	defer func() { close(stop); readers.Wait() }()
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				var got record
				if err := s.Get("tokens", "one", &got); err != nil {
					t.Error(err)
					return
				}
				if got.Label != fmt.Sprint(got.Generation) || got.Revoked != (got.Generation%2 == 1) {
					t.Errorf("torn record: %+v", got)
					return
				}
			}
		})
	}
	for generation := 1; generation <= 12; generation++ {
		if err := write(generation); err != nil {
			t.Fatal(err)
		}
		var got record
		if err := s.Get("tokens", "one", &got); err != nil || got.Generation != generation || got.Revoked != (generation%2 == 1) {
			t.Fatalf("committed revocation/update not visible: %+v, %v", got, err)
		}
	}
}
