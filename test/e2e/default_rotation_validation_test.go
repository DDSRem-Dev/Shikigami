//go:build e2e

package e2e

import (
	"testing"
	"time"
)

// This clock is only input data for the acceptance observer's decision rule.
// It never reaches a production host or the credential issuer.
func TestDefaultRotationTimingAndEpochValidation(t *testing.T) {
	lower := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	original := rotationProcessIdentity{Role: "client-port", PID: 41, Created: 100, Executable: "/fixture/shiki-plugin-port", UID: 1000, Parent: 20, SHA256: "test-artifact-hash"}
	epoch := rotationEpoch{Role: original.Role, Lower: lower, Ready: lower.Add(800 * time.Millisecond), LastPresent: lower.Add(defaultRotationTTL), Original: original, Current: original}
	next := rotationProcessIdentity{Role: original.Role, PID: 42, Created: 200, Executable: original.Executable, UID: original.UID, Parent: original.Parent, SHA256: original.SHA256}
	tests := []struct {
		name   string
		when   time.Time
		mutate func(*rotationEpoch, *rotationProcessIdentity)
		valid  bool
	}{
		{"ambiguous retirement interval", lower.Add(defaultRotationTTL + time.Second), func(e *rotationEpoch, _ *rotationProcessIdentity) {
			e.LastPresent = lower.Add(defaultRotationTTL - time.Nanosecond)
		}, false},
		{"one nanosecond early", lower.Add(defaultRotationTTL - time.Nanosecond), nil, false},
		{"earliest possible natural expiry", lower.Add(defaultRotationTTL), nil, true},
		{"after first ready upper bound", epoch.Ready.Add(defaultRotationTTL + time.Second), nil, true},
		{"bounded recovery endpoint", epoch.Ready.Add(defaultRotationTTL + defaultRotationRecovery), nil, true},
		{"overdue replacement", epoch.Ready.Add(defaultRotationTTL + defaultRotationRecovery + time.Nanosecond), nil, false},
		{"unchanged worker", lower.Add(defaultRotationTTL), func(e *rotationEpoch, n *rotationProcessIdentity) { *n = e.Original }, false},
		{"reused original pid", lower.Add(defaultRotationTTL), func(e *rotationEpoch, n *rotationProcessIdentity) { n.PID = e.Original.PID }, false},
		{"same creation epoch", lower.Add(defaultRotationTTL), func(e *rotationEpoch, n *rotationProcessIdentity) { n.Created = e.Original.Created }, false},
		{"older process", lower.Add(defaultRotationTTL), func(e *rotationEpoch, n *rotationProcessIdentity) { n.Created = e.Original.Created - 1 }, false},
		{"wrong executable", lower.Add(defaultRotationTTL), func(_ *rotationEpoch, n *rotationProcessIdentity) { n.Executable = "/other/worker" }, false},
		{"wrong UID", lower.Add(defaultRotationTTL), func(_ *rotationEpoch, n *rotationProcessIdentity) { n.UID++ }, false},
		{"wrong parent", lower.Add(defaultRotationTTL), func(_ *rotationEpoch, n *rotationProcessIdentity) { n.Parent++ }, false},
		{"wrong artifact hash", lower.Add(defaultRotationTTL), func(_ *rotationEpoch, n *rotationProcessIdentity) { n.SHA256 = "changed" }, false},
		{"wrong role", lower.Add(defaultRotationTTL), func(_ *rotationEpoch, n *rotationProcessIdentity) { n.Role = "server-port" }, false},
		{"second replacement", lower.Add(defaultRotationTTL), func(e *rotationEpoch, _ *rotationProcessIdentity) { e.Replacements = 1 }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e, n := epoch, next
			if test.mutate != nil {
				test.mutate(&e, &n)
			}
			err := rotationNextEpoch(e, n, test.when)
			if (err == nil) != test.valid {
				t.Fatalf("decision=%v valid=%v", err, test.valid)
			}
		})
	}
}

func TestDefaultRotationHeldTCPDecisionRejectsEarlyTimeoutAndReplay(t *testing.T) {
	earliest := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	latest := earliest.Add(30 * time.Second)
	for _, test := range []struct {
		name   string
		result rotationTCPResult
		valid  bool
	}{
		{"natural EOF", rotationTCPResult{At: earliest}, true},
		{"valid EOF-only tail", rotationTCPResult{At: latest, Tail: []byte(rotationTail)}, true},
		{"early EOF", rotationTCPResult{At: earliest.Add(-time.Nanosecond)}, false},
		{"late EOF", rotationTCPResult{At: latest.Add(time.Nanosecond)}, false},
		{"deadline error", rotationTCPResult{At: earliest, Error: "i/o timeout"}, false},
		{"replayed application data", rotationTCPResult{At: earliest, Tail: []byte("old-payload")}, false},
		{"partial EOF tail", rotationTCPResult{At: earliest, Tail: []byte(rotationTail[:3])}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := rotationTCPTerminationAllowed(test.result, earliest, latest)
			if (err == nil) != test.valid {
				t.Fatalf("decision=%v valid=%v", err, test.valid)
			}
		})
	}
}
