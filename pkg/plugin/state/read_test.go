package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type retryState struct {
	Reason string            `json:"reason"`
	Next   int64             `json:"next,omitempty"`
	Signal int               `json:"signal,omitempty"`
	Exit   *int              `json:"exit,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	Queue  []string          `json:"queue,omitempty"`
}

func TestGetReplacesCompleteRecordWithoutStaleOmittedFields(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "state"), Options{})
	exit := 7
	known := retryState{Reason: "known", Next: 123, Signal: 15, Exit: &exit, Labels: map[string]string{"old": "value"}, Queue: []string{"old"}}
	if err := s.Update(func(snapshot *Snapshot) error { return snapshot.Put("retry", "one", known) }); err != nil {
		t.Fatal(err)
	}
	first, err := s.View()
	if err != nil {
		t.Fatal(err)
	}
	var target retryState
	if err := first.Get("retry", "one", &target); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(target, known) {
		t.Fatal(target)
	}
	unknown := retryState{Reason: "unknown"}
	if err := s.Update(func(snapshot *Snapshot) error { return snapshot.Put("retry", "one", unknown) }); err != nil {
		t.Fatal(err)
	}
	second, err := s.View()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(second.Buckets["retry"]["one"]); got != `{"reason":"unknown"}` {
		t.Fatalf("persisted record unexpectedly retained fields: %s", got)
	}
	if err := second.Get("retry", "one", &target); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(target, unknown) {
		t.Fatalf("read merged stale fields into replacement: %+v", target)
	}
}

func TestGetReplacesMapsSlicesAndPointersAndPreservesFailedTargets(t *testing.T) {
	snapshot := &Snapshot{Buckets: map[string]map[string]json.RawMessage{"values": {
		"map": json.RawMessage(`{"new":"value"}`), "slice": json.RawMessage(`["new"]`),
		"record": json.RawMessage(`{"reason":"new"}`), "null": json.RawMessage(`null`),
		"invalid": json.RawMessage(`{"reason":"changed","next":"invalid"}`),
	}}}
	originalMap := map[string]string{"old": "value"}
	mapping := originalMap
	if err := snapshot.Get("values", "map", &mapping); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mapping, map[string]string{"new": "value"}) || !reflect.DeepEqual(originalMap, map[string]string{"old": "value"}) {
		t.Fatal("map was merged or old alias mutated", mapping, originalMap)
	}
	originalSlice := []string{"old", "tail"}
	list := originalSlice
	if err := snapshot.Get("values", "slice", &list); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list, []string{"new"}) || originalSlice[0] != "old" {
		t.Fatal("slice replacement mutated old alias")
	}
	original := &retryState{Reason: "old", Signal: 15}
	pointer := original
	if err := snapshot.Get("values", "record", &pointer); err != nil {
		t.Fatal(err)
	}
	if pointer == original || pointer.Reason != "new" || pointer.Signal != 0 || original.Reason != "old" {
		t.Fatal("pointer replacement retained stale record or changed its alias")
	}
	if err := snapshot.Get("values", "null", &pointer); err != nil || pointer != nil {
		t.Fatal("null did not clear pointer", err)
	}
	for _, id := range []string{"missing", "invalid"} {
		target := retryState{Reason: "keep", Next: 99, Signal: 15}
		before := target
		err := snapshot.Get("values", id, &target)
		if err == nil || !reflect.DeepEqual(target, before) {
			t.Fatalf("failed %s read partially modified target: %+v, %v", id, target, err)
		}
		if id == "missing" && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	for _, target := range []any{nil, retryState{}, (*retryState)(nil)} {
		var invalid *json.InvalidUnmarshalError
		if err := snapshot.Get("values", "record", target); !errors.As(err, &invalid) {
			t.Fatalf("invalid destination error changed: %v", err)
		}
	}
}

type customRecord struct {
	Value string
	Calls int
}

func (r *customRecord) UnmarshalJSON(data []byte) error {
	r.Calls++
	return json.Unmarshal(data, &r.Value)
}

func TestGetUsesCustomJSONOnFreshDestination(t *testing.T) {
	snapshot := &Snapshot{Buckets: map[string]map[string]json.RawMessage{"values": {"one": json.RawMessage(`"new"`)}}}
	target := customRecord{Value: "old", Calls: 9}
	if err := snapshot.Get("values", "one", &target); err != nil {
		t.Fatal(err)
	}
	if target.Value != "new" || target.Calls != 1 {
		t.Fatalf("custom decoder did not receive a fresh destination: %+v", target)
	}
}
