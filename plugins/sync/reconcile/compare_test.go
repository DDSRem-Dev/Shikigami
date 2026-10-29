package reconcile

import (
	"crypto/sha256"
	"fmt"
	"testing"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

func observed(content string) *pb.FileVersion {
	return &pb.FileVersion{Exists: true, ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(content))), Mode: 0600, Revision: "observed-" + content}
}
func TestDecidePreservesIndependentEditsAndDeletes(t *testing.T) {
	base, left, right, missing := observed("base"), observed("local"), observed("remote"), &pb.FileVersion{}
	tests := []struct {
		name    string
		b, l, r *pb.FileVersion
		want    Action
	}{
		{"equal", base, left, left, Equal},
		{"upload", base, left, base, Upload},
		{"download", base, base, right, Download},
		{"both changed", base, left, right, Conflict},
		{"local delete remote edit", base, missing, right, Conflict},
		{"remote delete local edit", base, left, missing, Conflict},
		{"propagate local deletion", base, missing, base, Upload},
		{"propagate remote deletion", base, base, missing, Download},
		{"both deleted", base, missing, missing, Equal},
		{"independent new files", missing, left, right, Conflict},
		{"first local only", nil, left, missing, Initial},
		{"first remote only", nil, missing, right, Initial},
		{"first different", nil, left, right, Initial},
		{"first equal", nil, left, left, Equal},
		{"empty trees", nil, missing, missing, Equal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decide(tt.b, tt.l, tt.r)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	// chmod is a real local edit; an opaque revision is not cross-machine content.
	changed := proto.Clone(base).(*pb.FileVersion)
	changed.Mode = 0700
	if got, err := Decide(base, changed, base); got != Upload || err != nil {
		t.Fatal(got, err)
	}
	changed.Mode = base.Mode
	changed.Revision = "different-device-observation"
	if got, err := Decide(base, changed, base); got != Equal || err != nil {
		t.Fatal(got, err)
	}
}
func TestUnknownObservationNeverBecomesDeletion(t *testing.T) {
	for _, bad := range []*pb.FileVersion{nil, {Exists: true}, {Exists: true, ContentHash: observed("x").ContentHash, Mode: 0600}, {Exists: false, ContentHash: "unexpected"}} {
		if _, err := Decide(observed("base"), bad, observed("remote")); err == nil {
			t.Fatal("invalid observation accepted")
		}
	}
}
func TestConflictGenerationRejectsStaleDisplayedChoice(t *testing.T) {
	b, l, r := observed("base"), observed("local"), observed("remote")
	first, err := Generation("mapping", "file", b, l, r)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Generation("mapping", "file", b, l, r)
	if first != again {
		t.Fatal("generation is unstable")
	}
	r.Revision += "-replaced"
	next, _ := Generation("mapping", "file", b, l, r)
	if next == first {
		t.Fatal("remote replacement retained generation")
	}
	different, _ := Generation("other-mapping", "file", b, l, r)
	if next == different {
		t.Fatal("generation is not scoped")
	}
}
