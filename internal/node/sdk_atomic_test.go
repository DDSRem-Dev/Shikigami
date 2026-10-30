package node

import (
	"testing"

	gvisorsync "gvisor.dev/gvisor/pkg/sync"
)

// The netstack's park callbacks depend on this CAS in race builds too. An
// ARM64 assembly regression compared against the replacement instead of old,
// preventing goroutines from parking and making embedded-node tests spin.
func TestNetstackAtomicCompareAndSwap(t *testing.T) {
	for _, tc := range []struct {
		name               string
		initial, old, next uintptr
		wantSwap           bool
		wantValue          uintptr
	}{
		{"replace", 1, 1, 2, true, 2},
		{"reject-without-mutation", 2, 1, 2, false, 2},
		{"clear", 2, 2, 0, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.initial
			swapped := gvisorsync.RaceUncheckedAtomicCompareAndSwapUintptr(&value, tc.old, tc.next)
			if swapped != tc.wantSwap || value != tc.wantValue {
				t.Fatalf("CAS returned (%v, %d), want (%v, %d)", swapped, value, tc.wantSwap, tc.wantValue)
			}
		})
	}
}
