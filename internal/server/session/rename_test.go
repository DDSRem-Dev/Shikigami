package session

import (
	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"testing"
)

func TestRenamePreservesSessionIdentityAndRejectsProjectDuplicate(t *testing.T) {
	m, _ := managerFixture(t, 3)
	first, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "first", RequestId: "rename-first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "second", RequestId: "rename-second"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := m.tmux.Inspect(t.Context(), first.Id)
	if err != nil {
		t.Fatal(err)
	}
	request := &pb.UpdateSessionRequest{Id: first.Id, Name: second.Name, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}}}
	if _, err := m.Update(t.Context(), request); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatal("duplicate name accepted", err)
	}
	request.Name = "renamed"
	renamed, err := m.Update(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	after, err := m.tmux.Inspect(t.Context(), first.Id)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Id != first.Id || renamed.Name != "renamed" || after.PID != before.PID || after.PaneID != before.PaneID || after.TmuxSessionID != before.TmuxSessionID {
		t.Fatal("rename changed process identity", renamed, after)
	}
	request.Name = "invalid\nname"
	if _, err := m.Update(t.Context(), request); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("invalid name accepted", err)
	}
}
