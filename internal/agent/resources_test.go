package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	agentrpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	pluginpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

type authenticatedProbe struct {
	daemonProbe
	id              string
	token           atomic.Value
	calls           atomic.Int64
	capabilities    []string
	portCheckDenied bool
}

func (p *authenticatedProbe) NegotiateProtocol(context.Context, *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	return &pb.ProtocolAgreement{Major: 1, Capabilities: p.capabilities}, nil
}
func (p *authenticatedProbe) GetSystemInfo(context.Context, *pb.Empty) (*pb.SystemInfo, error) {
	return &pb.SystemInfo{DaemonId: p.id}, nil
}
func (p *authenticatedProbe) GetProject(_ context.Context, in *pb.GetProjectRequest) (*pb.Project, error) {
	return &pb.Project{Id: in.Id, Name: "remote-project"}, nil
}
func (p *authenticatedProbe) authenticate(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		info, _ := connect.CallInfoForServerContext(ctx)
		if info.RequestHeader().Get("Authorization") != "Bearer "+p.token.Load().(string) {
			return rpcError(connect.CodeUnauthenticated, "must-not-leak-credential")
		}
		if info.RequestHeader().Get(ServerHeader) != "" || info.RequestHeader().Get(FrontendHeader) != "" {
			return rpcError(connect.CodePermissionDenied, "LOCAL_HEADER_LEAK")
		}
		p.calls.Add(1)
		return next(ctx, spec, stream)
	}
}
func tlsProbe(t *testing.T, id, token string) (*authenticatedProbe, *agentpb.ServerConfig) {
	t.Helper()
	p := &authenticatedProbe{daemonProbe: daemonProbe{active: map[string]chan struct{}{}}, id: id, capabilities: []string{"terminal.v1"}}
	p.token.Store(token)
	server := connect.NewServer(p.authenticate)
	rpc.RegisterShikiDaemonHandler(server, p)
	ts := httptest.NewUnstartedServer(connection.NewRPCHandler(server))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	return p, &agentpb.ServerConfig{Name: id, DirectTlsAddress: strings.TrimPrefix(ts.URL, "https://"), TlsServerName: "example.com", TlsCaFile: ca}
}
func resourceConfig(t *testing.T) *config.Client {
	t.Helper()
	cfg := config.DefaultClient()
	root := t.TempDir()
	cfg.Agent.StateDir = filepath.Join(root, "agent")
	cfg.Tailscale.StateDir = filepath.Join(root, "node")
	cfg.Tailscale.Enabled = false
	return cfg
}
func openResourceService(t *testing.T, ctx context.Context, cfg *config.Client) (*Service, *Resources) {
	t.Helper()
	s := NewService(ctx, uint32(os.Getuid()), "test", nil)
	r, err := OpenResources(ctx, s, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}
func findServer(t *testing.T, s *Service, id string) *agentpb.ServerRecord {
	t.Helper()
	list, err := s.ListServers(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range list.Servers {
		if record.Config.Id == id {
			return record
		}
	}
	t.Fatal("server missing")
	return nil
}
func addStoredServer(t *testing.T, ctx context.Context, client agentrpc.AgentClient, cfg *agentpb.ServerConfig, token string) *agentpb.ServerRecord {
	t.Helper()
	record, err := client.UpsertServer(ctx, &agentpb.UpsertServerRequest{Config: cfg, RequestId: newID("request_")})
	if err != nil {
		t.Fatal(err)
	}
	record, err = client.SetServerCredential(ctx, &agentpb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: []byte(token), RequestId: newID("request_")})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestResourcesTLSIdentityPersistenceAndCredentialCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := resourceConfig(t)
	s, r := openResourceService(t, ctx, cfg)
	closed := false
	defer func() {
		if !closed {
			r.Close()
		}
	}()
	local := serveUnix(t, s.Handler(s.RPCServer()), ctx)
	raw := connection.NewRPCClient(local)
	client := agentrpc.NewAgentClient(raw)
	front := handshake(t, client)
	localCtx := callContext(ctx, front, "")
	proxy := rpc.NewShikiDaemonClient(raw)
	probeA, configA := tlsProbe(t, "daemon_alpha", "shiki_secret_alpha")
	_, configB := tlsProbe(t, "daemon_beta", "shiki_secret_beta")
	a := addStoredServer(t, localCtx, client, configA, "shiki_secret_alpha")
	b := addStoredServer(t, localCtx, client, configB, "shiki_secret_beta")
	for _, pair := range []struct{ id, want string }{{a.Config.Id, "daemon_alpha"}, {b.Config.Id, "daemon_beta"}} {
		info, err := proxy.GetSystemInfo(callContext(ctx, front, pair.id), &pb.Empty{})
		if err != nil || info.GetDaemonId() != pair.want {
			t.Fatalf("isolated TLS routing: %v", err)
		}
	}
	a = findServer(t, s, a.Config.Id)
	if a.Config.DaemonId != "daemon_alpha" || a.ConnectionState != "ready" {
		t.Fatal("authenticated identity not pinned")
	}
	mappingRequest := &agentpb.PutMappingRequest{RequestId: "create-mapping", Mapping: &pluginpb.Mapping{ServerId: a.Config.Id, ProjectId: "proj_remote", LocalRoot: t.TempDir()}}
	mapping, err := client.PutMapping(localCtx, mappingRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.RemoveServer(localCtx, &agentpb.RemoveServerRequest{ServerId: a.Config.Id, ExpectedRevision: a.Revision, RequestId: "remove-in-use"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("removed associated server: %v", err)
	}
	changed := proto.Clone(a.Config).(*agentpb.ServerConfig)
	changed.DaemonId = "daemon_beta"
	if _, err = client.UpsertServer(localCtx, &agentpb.UpsertServerRequest{Config: changed, ExpectedRevision: a.Revision, RequestId: "replace-identity"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("replaced associated identity: %v", err)
	}
	// Two writes based on one revision cannot both commit.
	var wg sync.WaitGroup
	codes := make(chan connect.Code, 2)
	for _, name := range []string{"alpha-one", "alpha-two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			next := proto.Clone(a.Config).(*agentpb.ServerConfig)
			next.Name = name
			_, err := client.UpsertServer(callContext(ctx, front, ""), &agentpb.UpsertServerRequest{Config: next, ExpectedRevision: a.Revision, RequestId: name})
			if err == nil {
				codes <- 0
			} else {
				codes <- connect.CodeOf(err)
			}
		}(name)
	}
	wg.Wait()
	close(codes)
	success, aborted := 0, 0
	for code := range codes {
		if code == 0 {
			success++
		}
		if code == connect.CodeAborted {
			aborted++
		}
	}
	if success != 1 || aborted != 1 {
		t.Fatalf("revision arbitration: success=%d aborted=%d", success, aborted)
	}
	stream, err := proxy.TerminalStream(callContext(ctx, front, a.Config.Id))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "sess_remote"}}}); err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Receive()
	if err != nil {
		t.Fatal(err)
	}
	probeA.token.Store("shiki_secret_replacement")
	replacement := &agentpb.SetServerCredentialRequest{ServerId: a.Config.Id, Token: []byte("shiki_secret_replacement"), ExpectedCredentialRevision: a.CredentialRevision, RequestId: "replace-credential"}
	updated, err := client.SetServerCredential(localCtx, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Receive(); err == nil {
		t.Fatal("credential rotation retained old stream")
	}
	probeA.waitClosed(t, ready.AttachmentId)
	if _, err = proxy.GetSystemInfo(callContext(ctx, front, a.Config.Id), &pb.Empty{}); err != nil {
		t.Fatal(err)
	}
	replay, err := client.SetServerCredential(localCtx, replacement)
	if err != nil || replay.CredentialRevision != updated.CredentialRevision {
		t.Fatal("credential replay changed result")
	}
	mismatched := proto.Clone(replacement).(*agentpb.SetServerCredentialRequest)
	mismatched.Token = []byte("shiki_other")
	if _, err = client.SetServerCredential(localCtx, mismatched); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("request_id accepted different secret")
	}
	prefs, err := client.GetPreferences(localCtx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	prefs.DefaultServerId = b.Config.Id
	prefs.NoColor = true
	savedPrefs, err := client.SetPreferences(localCtx, &agentpb.SetPreferencesRequest{Values: prefs, RequestId: "preferences"})
	if err != nil {
		t.Fatal(err)
	}
	// No response, event or generic state record may expose token bytes.
	wire, _ := json.Marshal(updated)
	for _, secret := range []string{"shiki_secret_alpha", "shiki_secret_replacement"} {
		if bytes.Contains(wire, []byte(secret)) {
			t.Fatal("credential response leaked secret")
		}
	}
	if err = filepath.WalkDir(filepath.Join(cfg.Agent.StateDir, "resources"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, secret := range []string{"shiki_secret_alpha", "shiki_secret_replacement"} {
			if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte(base64.StdEncoding.EncodeToString([]byte(secret)))) {
				t.Fatal("generic persistence leaked secret")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	// Configuration remains an import source only; IPC state owns later restarts.
	cfg.Servers["must-not-import-again"] = config.Server{DirectTLSAddress: configA.DirectTlsAddress, TLSServerName: configA.TlsServerName, TLSCAFile: configA.TlsCaFile}
	next, restored := openResourceService(t, ctx, cfg)
	defer restored.Close()
	after, err := next.ListServers(ctx, &pb.Empty{})
	if err != nil || len(after.Servers) != 2 {
		t.Fatal("restart reimported or lost server state")
	}
	replay, err = next.SetServerCredential(ctx, replacement)
	if err != nil || replay.CredentialRevision != updated.CredentialRevision {
		t.Fatal("restart lost credential deduplication")
	}
	maps, err := next.ListMappings(ctx, &agentpb.ListMappingsRequest{ServerId: a.Config.Id})
	if err != nil || len(maps.Mappings) != 1 || maps.Mappings[0].Id != mapping.Id {
		t.Fatal("restart lost mapping")
	}
	persistedPrefs, err := next.GetPreferences(ctx, &pb.Empty{})
	if err != nil || !proto.Equal(persistedPrefs, savedPrefs) {
		t.Fatal("restart lost preferences")
	}
	// An address update preserves the pinned identity. A different authenticated
	// daemon at that address must be rejected before proxying application traffic.
	current := findServer(t, next, a.Config.Id)
	addressChange := proto.Clone(current.Config).(*agentpb.ServerConfig)
	addressChange.DirectTlsAddress = configB.DirectTlsAddress
	addressChange.TlsCaFile = configB.TlsCaFile
	if _, err = next.UpsertServer(ctx, &agentpb.UpsertServerRequest{Config: addressChange, ExpectedRevision: current.Revision, RequestId: "change-address"}); err != nil {
		t.Fatal(err)
	}
	current = findServer(t, next, a.Config.Id)
	if _, err = next.SetServerCredential(ctx, &agentpb.SetServerCredentialRequest{ServerId: a.Config.Id, Token: []byte("shiki_secret_beta"), ExpectedCredentialRevision: current.CredentialRevision, RequestId: "beta-credential"}); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Resolve(ctx, a.Config.Id); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("accepted another daemon identity: %v", err)
	}
}

func TestResourcesFailClosedOnCorruption(t *testing.T) {
	for _, kind := range []string{"resources", "credential"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			cfg := resourceConfig(t)
			s, r := openResourceService(t, ctx, cfg)
			record, err := s.UpsertServer(ctx, &agentpb.UpsertServerRequest{Config: &agentpb.ServerConfig{Name: "local", DirectTlsAddress: "127.0.0.1:1", TlsServerName: "localhost"}, RequestId: "create"})
			if err != nil {
				t.Fatal(err)
			}
			record, err = s.SetServerCredential(ctx, &agentpb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: []byte("shiki_private"), RequestId: "credential"})
			if err != nil {
				t.Fatal(err)
			}
			if err = r.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cfg.Agent.StateDir, "resources", "state.json")
			if kind == "credential" {
				path = filepath.Join(cfg.Agent.StateDir, "credentials", record.CredentialRevision)
			}
			if err = os.WriteFile(path, []byte{}, 0600); err != nil {
				t.Fatal(err)
			}
			next := NewService(ctx, uint32(os.Getuid()), "test", nil)
			if reopened, err := OpenResources(ctx, next, cfg, nil); err == nil {
				reopened.Close()
				t.Fatal("corrupt state reopened")
			}
			data, err := os.ReadFile(path)
			if err != nil || len(data) != 0 {
				t.Fatal("corrupt evidence was overwritten")
			}
		})
	}
}

func (p *authenticatedProbe) CheckPort(_ context.Context, r *pb.CheckPortRequest) (*pb.CheckPortResponse, error) {
	if p.portCheckDenied {
		return nil, rpcError(connect.CodePermissionDenied, "PORT_TARGET_NOT_ALLOWED")
	}
	return &pb.CheckPortResponse{Port: &pb.OpenPort{Port: r.Port, BindAddress: r.RemoteAddress}}, nil
}
