package session

import (
	"testing"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestSampleTargetsUsesFreshVerifiedPane(t *testing.T) {
	m, _ := managerFixture(t, 3)
	value, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "metrics", RequestId: "metrics"})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := m.SampleTargets(t.Context(), value.Id)
	if err != nil || len(targets) != 1 || targets[0].PID <= 0 {
		t.Fatalf("live target: %v %v", targets, err)
	}
	if err := m.store.Update(func(state *storage.Snapshot) error {
		value.TmuxPane = "%99999999"
		return state.Put(storage.SessionsBucket, value.Id, value)
	}); err != nil {
		t.Fatal(err)
	}
	targets, err = m.SampleTargets(t.Context(), value.Id)
	if err != nil || targets[0].PID != 0 {
		t.Fatalf("mismatched or persisted PID accepted: %v %v", targets, err)
	}
}
