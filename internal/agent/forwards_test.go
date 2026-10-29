package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

func forwardResources(t *testing.T) (*Service, *Resources, *apb.ServerRecord) {
	return forwardResourcesWithPolicy(t, false)
}
func forwardResourcesWithPolicy(t *testing.T, denied bool) (*Service, *Resources, *apb.ServerRecord) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	s, r := openResourceService(t, ctx, resourceConfig(t))
	t.Cleanup(func() { r.Close() })
	probe, cfg := tlsProbe(t, "daemon_forward", "shiki_forward_test")
	probe.capabilities = []string{sdk.PortCapability, sdk.PortCheckCapability}
	probe.portCheckDenied = denied
	record, err := s.UpsertServer(ctx, &apb.UpsertServerRequest{Config: cfg, RequestId: "server"})
	if err != nil {
		t.Fatal(err)
	}
	record, err = s.SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: []byte("shiki_forward_test"), RequestId: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	return s, r, record
}
func createRequest(server string) *apb.CreateForwardRequest {
	return &apb.CreateForwardRequest{RequestId: "create-forward", Spec: &ppb.ForwardSpec{ServerId: server, RemotePort: 3000, Enabled: true}}
}
func TestForwardIntentReplayStopAndServerIdentityGuard(t *testing.T) {
	s, r, server := forwardResources(t)
	ctx := context.Background()
	request := createRequest(server.Config.Id)
	first, err := s.CreateForward(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Spec.Id == "" || first.Spec.Revision == "" || first.State != "error" || first.BoundPort != 0 {
		t.Fatal("missing plugin fabricated a listener", first)
	}
	repeat, err := s.CreateForward(ctx, request)
	if err != nil || !proto.Equal(first, repeat) {
		t.Fatal("request replay changed result", repeat, err)
	}
	changed := proto.Clone(request).(*apb.CreateForwardRequest)
	changed.Spec.RemotePort = 4000
	if _, err = s.CreateForward(ctx, changed); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("reused request accepted different endpoint", err)
	}
	list, err := s.ListForwards(ctx, &pb.Empty{})
	if err != nil || len(list.Forwards) != 1 {
		t.Fatal(list, err)
	}
	current := findServer(t, s, server.Config.Id)
	if _, err = s.RemoveServer(ctx, &apb.RemoveServerRequest{ServerId: current.Config.Id, ExpectedRevision: current.Revision, RequestId: "remove-used"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("removed server with forward", err)
	}
	updated := proto.Clone(current.Config).(*apb.ServerConfig)
	updated.DaemonId = "daemon_other"
	if _, err = s.UpsertServer(ctx, &apb.UpsertServerRequest{Config: updated, ExpectedRevision: current.Revision, RequestId: "retarget"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("retargeted live desired forward", err)
	}
	stop := &apb.StopForwardRequest{ForwardId: first.Spec.Id, ExpectedRevision: "stale", RequestId: "stop"}
	if _, err = s.StopForward(ctx, stop); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatal(err)
	}
	stop.ExpectedRevision = first.Spec.Revision
	if _, err = s.StopForward(ctx, stop); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StopForward(ctx, stop); err != nil {
		t.Fatal("stop replay not stable", err)
	}
	repeat, err = s.CreateForward(ctx, request)
	if err != nil || !proto.Equal(first, repeat) {
		t.Fatal("create replay after stop changed result", err)
	}
	list, err = s.ListForwards(ctx, &pb.Empty{})
	if err != nil || len(list.Forwards) != 0 {
		t.Fatal("replay restored stopped listener", list, err)
	}
	snapshot, _ := r.store.View()
	if len(snapshot.Buckets["forwards"]) != 0 {
		t.Fatal("stopped desired state remains")
	}
	if _, err = s.RemoveServer(ctx, &apb.RemoveServerRequest{ServerId: current.Config.Id, ExpectedRevision: current.Revision, RequestId: "remove-unused"}); err != nil {
		t.Fatal(err)
	}
}
func TestForwardRejectsUntrustedEndpointAndExactCoreScope(t *testing.T) {
	s, _, server := forwardResources(t)
	ctx := context.Background()
	for _, address := range []string{"0.0.0.0", "localhost", "192.168.1.1", "::"} {
		request := createRequest(server.Config.Id)
		request.Spec.RemoteAddress = address
		if _, err := s.CreateForward(ctx, request); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatal(address, err)
		}
	}
	request := createRequest(server.Config.Id)
	state, err := s.CreateForward(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := s.requireResources()
	core := &pluginCore{resources: r, id: "port"}
	scope := &ppb.Scope{ServerId: server.Config.Id}
	frame := &pb.PortData{Type: pb.PortData_CONNECT, RemoteAddress: "127.0.0.1", Port: 3000, ConnectionId: "test"}
	if err = core.authorizeForward(ctx, scope, frame); err != nil {
		t.Fatal(err)
	}
	frame.Port = 3001
	if err = core.authorizeForward(ctx, scope, frame); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("arbitrary endpoint proxied", err)
	}
	frame.Port = 3000
	scope.SessionId = "sess_other"
	if err = core.authorizeForward(ctx, scope, frame); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("wrong session accepted", err)
	}
	scope.SessionId = ""
	scope.MappingId = "map_other"
	if err = core.authorizeForward(ctx, scope, frame); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("file scope crossed into ports", err)
	}
	scope.MappingId = ""
	if _, err = s.StopForward(ctx, &apb.StopForwardRequest{ForwardId: state.Spec.Id, ExpectedRevision: state.Spec.Revision, RequestId: "stop"}); err != nil {
		t.Fatal(err)
	}
	if err = core.authorizeForward(ctx, scope, frame); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("stopped endpoint remained authorized", err)
	}
}
func TestPendingCreateCannotResurrectStoppedDesiredState(t *testing.T) {
	s, r, server := forwardResources(t)
	ctx := context.Background()
	request := createRequest(server.Config.Id)
	state, err := s.CreateForward(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.store.Update(func(snapshot *storage.Snapshot) error {
		record := snapshot.Requests["CreateForward\x00"+request.RequestId]
		record.Status = "pending"
		record.Result, _ = json.Marshal(&ppb.ForwardState{Spec: state.Spec, State: "starting"})
		snapshot.Requests["CreateForward\x00"+request.RequestId] = record
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StopForward(ctx, &apb.StopForwardRequest{ForwardId: state.Spec.Id, ExpectedRevision: state.Spec.Revision, RequestId: "stop"}); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.CreateForward(ctx, request)
	if err != nil || resumed.State != "stopped" || resumed.BoundPort != 0 {
		t.Fatal("pending create resurrected a stopped resource", resumed, err)
	}
}
func TestPendingStopKeepsDisabledIntentUntilCleanup(t *testing.T) {
	s, r, server := forwardResources(t)
	ctx := context.Background()
	state, err := s.CreateForward(ctx, createRequest(server.Config.Id))
	if err != nil {
		t.Fatal(err)
	}
	stop := &apb.StopForwardRequest{ForwardId: state.Spec.Id, ExpectedRevision: state.Spec.Revision, RequestId: "pending-stop"}
	encoded, _ := requestBytes(stop.RequestId, stop)
	if err = r.store.Update(func(snapshot *storage.Snapshot) error {
		record, _, e := snapshot.Request("StopForward", stop.RequestId, encoded)
		if e != nil {
			return e
		}
		record.ResourceID = state.Spec.Id
		record.Result = []byte("{}")
		snapshot.Requests["StopForward\x00"+stop.RequestId] = record
		spec, e := forwardSpec(snapshot, state.Spec.Id)
		if e != nil {
			return e
		}
		spec.Enabled = false
		spec.Revision = newID("rev_")
		return snapshot.Put("forwards", spec.Id, spec)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StopForward(ctx, stop); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := r.store.View()
	if len(snapshot.Buckets["forwards"]) != 0 || snapshot.Requests["StopForward\x00"+stop.RequestId].Status != "succeeded" {
		t.Fatal("pending stop not finished")
	}
}

func TestDeletedSessionRemovesDesiredForwardAndGrant(t *testing.T) {
	s, r, server := forwardResources(t)
	ctx := context.Background()
	state, err := s.CreateForward(ctx, createRequest(server.Config.Id))
	if err != nil {
		t.Fatal(err)
	}
	// Model a session-scoped record left by a previously valid session.
	state.Spec.SessionId = "sess_deleted"
	if err = r.store.Update(func(snapshot *storage.Snapshot) error {
		return snapshot.Put("forwards", state.Spec.Id, state.Spec)
	}); err != nil {
		t.Fatal(err)
	}
	core := &pluginCore{resources: r, id: "port"}
	scope := &ppb.Scope{ServerId: server.Config.Id, SessionId: "sess_deleted"}
	if err = core.authorizePort(scope); err != nil {
		t.Fatal("fixture did not establish a session-scoped grant", err)
	}
	r.forwardMu.Lock()
	err = r.disableDeletedSessionForward(ctx, state.Spec)
	r.forwardMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListForwards(ctx, &pb.Empty{})
	if err != nil || len(list.Forwards) != 0 {
		t.Fatal("deleted session retained desired listener", list, err)
	}
	if err = core.authorizePort(scope); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("deleted session retained endpoint grant", err)
	}
	if _, err = s.CreateForward(ctx, createRequest(server.Config.Id)); err != nil {
		t.Fatal("original request replay failed", err)
	}
	list, err = s.ListForwards(ctx, &pb.Empty{})
	if err != nil || len(list.Forwards) != 0 {
		t.Fatal("request replay resurrected deleted session forward", list, err)
	}
}

func TestForwardPermissionPreflightDoesNotPersistDeniedIntent(t *testing.T) {
	s, r, server := forwardResourcesWithPolicy(t, true)
	if _, e := s.CreateForward(t.Context(), createRequest(server.Config.Id)); connect.CodeOf(e) != connect.CodePermissionDenied {
		t.Fatal("denied target persisted", e)
	}
	state, e := r.store.View()
	if e != nil || len(state.Buckets["forwards"]) != 0 {
		t.Fatal("denied authorization created intent", e)
	}
	if _, exists := state.Requests["CreateForward\x00create-forward"]; exists {
		t.Fatal("denied preflight accepted durable request")
	}
}
func TestAutomaticMetadataIsHostAssigned(t *testing.T) {
	s, _, server := forwardResources(t)
	request := createRequest(server.Config.Id)
	request.Spec.Automatic = true
	request.Spec.SourceIdentity = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, e := s.CreateForward(t.Context(), request); connect.CodeOf(e) != connect.CodeInvalidArgument {
		t.Fatal("frontend forged automatic authorization", e)
	}
}
