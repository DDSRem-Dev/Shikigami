package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

type extensionCLIProbe struct {
	oldAgent bool
	arpc.UnimplementedAgentHandler
	erpc.UnimplementedRegionServiceHandler
	erpc.UnimplementedBrowserServiceHandler
	erpc.UnimplementedBrowserLocalServiceHandler
	mu              sync.Mutex
	requests        []proto.Message
	mutations       []proto.Message
	profiles        []*epb.BrowserProfile
	fileData        []byte
	corruptDownload bool
	applyErr        error
	rollbackState   string
	uploadChunks    []*epb.BrowserFileChunk
}

func (p *extensionCLIProbe) Handshake(_ context.Context, request *apb.HandshakeRequest) (*apb.HandshakeResponse, error) {
	if len(request.GetProtocol().GetRequiredCapabilities()) != 1 || request.Protocol.RequiredCapabilities[0] != "agent.control.v1" {
		return nil, errors.New("extensions must not become mandatory for unrelated clients")
	}
	capabilities := []string{"agent.control.v1", "region.host.v1", "browser.remote.v1", "browser.viewer.v1"}
	if !p.oldAgent {
		capabilities = append(capabilities, "browser.launch-mode.v1")
	}
	return &apb.HandshakeResponse{InstanceId: "instance", FrontendId: "frontend", Protocol: &pb.ProtocolAgreement{Major: 1, Capabilities: capabilities}}, nil
}
func (p *extensionCLIProbe) GetStatus(context.Context, *pb.Empty) (*apb.AgentStatus, error) {
	return &apb.AgentStatus{InstanceId: "instance", State: "ready"}, nil
}
func (p *extensionCLIProbe) ReleaseFrontend(context.Context, *pb.Empty) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}
func (p *extensionCLIProbe) GetPreferences(context.Context, *pb.Empty) (*apb.Preferences, error) {
	return &apb.Preferences{DefaultServerId: "srv_cli"}, nil
}
func (p *extensionCLIProbe) ListServers(context.Context, *pb.Empty) (*apb.ServerList, error) {
	return &apb.ServerList{Servers: []*apb.ServerRecord{{Config: &apb.ServerConfig{Id: "srv_cli", Name: "cli-server"}, RemoteProtocol: &pb.ProtocolAgreement{Major: 1, Capabilities: []string{"region.host.v1", "browser.remote.v1", "browser.launch-mode.v1"}}}}}, nil
}
func (p *extensionCLIProbe) record(ctx context.Context, message proto.Message, server string, mutation bool) error {
	info, _ := connect.CallInfoForServerContext(ctx)
	h := info.RequestHeader()
	if h.Get("Shiki-Agent-Instance-Id") != "instance" || h.Get("Shiki-Frontend-Id") != "frontend" || h.Get("Shiki-Server-Id") != server || h.Get("Authorization") != "" {
		return errors.New("incorrect IPC identity or remote/local routing")
	}
	if scoped, ok := message.(interface{ GetServerId() string }); ok && scoped.GetServerId() != "srv_cli" {
		return errors.New("message did not use the resolved remote server ID")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, proto.Clone(message))
	if mutation {
		p.mutations = append(p.mutations, proto.Clone(message))
	}
	return nil
}
func (p *extensionCLIProbe) InspectRegion(ctx context.Context, request *epb.RegionInspectRequest) (*epb.RegionStatus, error) {
	if err := p.record(ctx, request, "srv_cli", false); err != nil {
		return nil, err
	}
	return &epb.RegionStatus{Revision: "host-v1", State: "ready", HelperAvailable: true, PendingOperationId: "pending_cli", ConfirmBefore: 1900000000}, nil
}
func (p *extensionCLIProbe) PlanRegion(ctx context.Context, request *epb.RegionPlanRequest) (*epb.RegionPlan, error) {
	if err := p.record(ctx, request, "srv_cli", false); err != nil {
		return nil, err
	}
	return &epb.RegionPlan{Id: "plan_cli", Revision: request.ExpectedRevision, Config: request.Config}, nil
}
func (p *extensionCLIProbe) ApplyRegion(ctx context.Context, request *epb.RegionApplyRequest) (*epb.RegionOperation, error) {
	if err := p.record(ctx, request, "srv_cli", true); err != nil {
		return nil, err
	}
	if !request.Confirm || request.DnsAuthorized {
		return nil, errors.New("confirmation or host-owned DNS authorization violated")
	}
	if p.applyErr != nil {
		return nil, p.applyErr
	}
	return &epb.RegionOperation{Id: "operation_cli", State: "pending_confirmation", Revision: "host-v2", ConfirmBefore: time.Now().Add(time.Minute).Unix()}, nil
}
func (p *extensionCLIProbe) ConfirmRegion(ctx context.Context, request *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	if err := p.record(ctx, request, "srv_cli", true); err != nil {
		return nil, err
	}
	if !request.Confirm {
		return nil, errors.New("confirmation omitted")
	}
	return &epb.RegionOperation{Id: request.OperationId, State: "confirmed"}, nil
}
func (p *extensionCLIProbe) RollbackRegion(ctx context.Context, request *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	if err := p.record(ctx, request, "srv_cli", true); err != nil {
		return nil, err
	}
	if !request.Confirm {
		return nil, errors.New("confirmation omitted")
	}
	state := p.rollbackState
	if state == "" {
		state = "rolled_back"
	}
	return &epb.RegionOperation{Id: request.OperationId, State: state}, nil
}
func (p *extensionCLIProbe) GetRegionOperation(ctx context.Context, request *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	if err := p.record(ctx, request, "srv_cli", false); err != nil {
		return nil, err
	}
	return &epb.RegionOperation{Id: request.OperationId, State: "applied"}, nil
}
func (p *extensionCLIProbe) DiagnoseRegion(ctx context.Context, request *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	if err := p.record(ctx, request, "srv_cli", false); err != nil {
		return nil, err
	}
	return &epb.DiagnosticReport{Observations: []*epb.DiagnosticObservation{{Check: "timezone", State: "unknown", Vantage: "remote-host", Limitation: "not a location or eligibility guarantee"}}}, nil
}
func (p *extensionCLIProbe) DiagnoseBrowser(ctx context.Context, request *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	if err := p.record(ctx, request, "srv_cli", false); err != nil {
		return nil, err
	}
	return &epb.DiagnosticReport{Observations: []*epb.DiagnosticObservation{{Check: "timezone", State: "pass", Vantage: "remote-chrome", Value: "Asia/Tokyo"}}}, nil
}
func (p *extensionCLIProbe) ListBrowserProfiles(ctx context.Context, request *epb.BrowserScope) (*epb.BrowserProfileList, error) {
	if err := p.record(ctx, request, "srv_cli", false); err != nil {
		return nil, err
	}
	return proto.Clone(&epb.BrowserProfileList{Profiles: p.profiles}).(*epb.BrowserProfileList), nil
}
func (p *extensionCLIProbe) PutBrowserProfile(ctx context.Context, request *epb.PutBrowserProfileRequest) (*epb.BrowserProfile, error) {
	if err := p.record(ctx, request, "srv_cli", true); err != nil {
		return nil, err
	}
	result := proto.Clone(request.Profile).(*epb.BrowserProfile)
	result.Revision = "profile-v2"
	return result, nil
}
func (p *extensionCLIProbe) DeleteBrowserProfile(ctx context.Context, request *epb.DeleteBrowserProfileRequest) (*epb.BrowserProfile, error) {
	if err := p.record(ctx, request, "srv_cli", true); err != nil {
		return nil, err
	}
	if !request.Confirm {
		return nil, errors.New("confirmation omitted")
	}
	return &epb.BrowserProfile{Id: request.ProfileId, State: "deleted"}, nil
}
func (p *extensionCLIProbe) StartBrowser(ctx context.Context, request *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	if err := p.record(ctx, request, "srv_cli", true); err != nil {
		return nil, err
	}
	if !request.Confirm {
		return nil, errors.New("confirmation omitted")
	}
	mode := "browse"
	if request.Diagnostics {
		mode = "diagnostic"
	}
	return &epb.BrowserStatus{ProfileId: request.ProfileId, InstanceId: "instance_cli", State: "running", Mode: mode}, nil
}
func (p *extensionCLIProbe) StopBrowser(ctx context.Context, request *epb.BrowserActionRequest) (*epb.BrowserStatus, error) {
	if err := p.record(ctx, request, "srv_cli", true); err != nil {
		return nil, err
	}
	if !request.Confirm {
		return nil, errors.New("confirmation omitted")
	}
	return &epb.BrowserStatus{ProfileId: request.ProfileId, State: "stopped"}, nil
}
func (p *extensionCLIProbe) GetBrowserStatus(ctx context.Context, request *epb.BrowserScope) (*epb.BrowserStatus, error) {
	if err := p.record(ctx, request, "srv_cli", false); err != nil {
		return nil, err
	}
	return &epb.BrowserStatus{ProfileId: request.ProfileId, State: "running", ControlRevision: "control-v1"}, nil
}
func (p *extensionCLIProbe) OpenBrowserViewer(ctx context.Context, request *epb.OpenBrowserViewerRequest) (*epb.BrowserViewer, error) {
	if err := p.record(ctx, request, "", false); err != nil {
		return nil, err
	}
	return &epb.BrowserViewer{Id: "viewer_cli", Url: "http://127.0.0.1:54321/bootstrap#private-bootstrap-test", State: "ready", ExpiresAt: time.Now().Add(time.Minute).Unix()}, nil
}
func (p *extensionCLIProbe) CloseBrowserViewer(ctx context.Context, request *epb.CloseBrowserViewerRequest) (*epb.BrowserViewer, error) {
	if err := p.record(ctx, request, "", false); err != nil {
		return nil, err
	}
	return &epb.BrowserViewer{Id: request.ViewerId, State: "closed"}, nil
}
func (p *extensionCLIProbe) ListBrowserFiles(ctx context.Context, request *epb.BrowserFileRequest) (*epb.BrowserFileList, error) {
	if err := p.record(ctx, request, "srv_cli", false); err != nil {
		return nil, err
	}
	return &epb.BrowserFileList{Files: []*epb.BrowserFile{{ProfileId: request.ProfileId, Area: request.Area, Name: "download.txt", Size: int64(len(p.fileData))}}}, nil
}
func (p *extensionCLIProbe) ReadBrowserFile(ctx context.Context, request *epb.BrowserFileRequest, stream erpc.BrowserServiceReadBrowserFileServerStream) error {
	if err := p.record(ctx, request, "srv_cli", true); err != nil {
		return err
	}
	sum := sha256.Sum256(p.fileData)
	checksum := hex.EncodeToString(sum[:])
	if p.corruptDownload {
		checksum = strings.Repeat("0", 64)
	}
	if err := stream.Send(&epb.BrowserFileChunk{ServerId: request.ServerId, ProfileId: request.ProfileId, Name: request.Name, TotalSize: int64(len(p.fileData)), Sha256: checksum}); err != nil {
		return err
	}
	for start := 0; start < len(p.fileData); start += browserFileChunkSize {
		end := min(start+browserFileChunkSize, len(p.fileData))
		if err := stream.Send(&epb.BrowserFileChunk{Data: p.fileData[start:end]}); err != nil {
			return err
		}
	}
	return stream.Send(&epb.BrowserFileChunk{Finish: true, Sha256: checksum})
}
func (p *extensionCLIProbe) WriteBrowserFile(ctx context.Context, stream erpc.BrowserServiceWriteBrowserFileServerStream) (*epb.BrowserFile, error) {
	var header *epb.BrowserFileChunk
	var data bytes.Buffer
	finished := false
	for {
		chunk, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header == nil {
			header = chunk
			if err := p.record(ctx, header, "srv_cli", true); err != nil {
				return nil, err
			}
		}
		if finished || len(chunk.Data) > browserFileChunkSize {
			return nil, errors.New("invalid upload framing")
		}
		p.mu.Lock()
		p.uploadChunks = append(p.uploadChunks, proto.Clone(chunk).(*epb.BrowserFileChunk))
		p.mu.Unlock()
		_, _ = data.Write(chunk.Data)
		if chunk.Finish {
			sum := sha256.Sum256(data.Bytes())
			if chunk.Sha256 != hex.EncodeToString(sum[:]) || int64(data.Len()) != header.TotalSize {
				return nil, errors.New("upload checksum or size mismatch")
			}
			finished = true
		}
	}
	if header == nil || !finished {
		return nil, errors.New("incomplete upload")
	}
	sum := sha256.Sum256(data.Bytes())
	return &epb.BrowserFile{ProfileId: header.ProfileId, Area: "uploads", Name: header.Name, Size: int64(data.Len()), Sha256: hex.EncodeToString(sum[:])}, nil
}

func extensionCLIFixture(t *testing.T, p *extensionCLIProbe) string {
	t.Helper()
	if p.profiles == nil {
		p.profiles = []*epb.BrowserProfile{{Id: "profile_cli", Name: "Private profile", Revision: "profile-v1", Region: &epb.RegionConfig{Country: "US", Timezone: "America/New_York", Locale: "en_US.UTF-8", Languages: []string{"en_US"}}}}
	}
	root, err := os.MkdirTemp("/tmp", "shiki-extensions-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	cfg := config.DefaultClient()
	cfg.Agent.Socket = filepath.Join(root, "agent.sock")
	cfg.Agent.StateDir = filepath.Join(root, "state")
	cfg.Agent.AutoStart = false
	cfg.Tailscale.Enabled = false
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := connection.ListenUnix(cfg.Agent.Socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	server := connect.NewServer()
	arpc.RegisterAgentHandler(server, p)
	erpc.RegisterRegionServiceHandler(server, p)
	erpc.RegisterBrowserServiceHandler(server, p)
	erpc.RegisterBrowserLocalServiceHandler(server, p)
	ctx, cancel := context.WithCancel(context.Background())
	httpServer := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	t.Cleanup(func() { cancel(); httpServer.Close(); <-done })
	return path
}

func TestRegionBrowserCriticalActionsRequireExplicitConfirmation(t *testing.T) {
	local := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(local, []byte("explicitly shared"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"region", "apply", "plan_cli", "--expected-revision", "host-v1"},
		{"region", "confirm", "operation_cli"},
		{"region", "rollback", "operation_cli"},
		{"browser", "start", "profile_cli", "--replace-running"},
		{"browser", "stop", "profile_cli"},
		{"browser", "profile", "create", "new_profile", "--name", "New profile"},
		{"browser", "profile", "update", "profile_cli", "--name", "Changed profile"},
		{"browser", "profile", "delete", "profile_cli"},
		{"browser", "profile", "boot", "profile_cli", "--replace-boot"},
		{"browser", "files", "upload", "profile_cli", local},
		{"browser", "files", "download", "profile_cli", "download.txt", "--output", filepath.Join(t.TempDir(), "retrieved.txt")},
	} {
		for _, confirmation := range []string{"missing", "--yes=false", "--yes"} {
			t.Run(strings.Join(args[:2], "/")+"/"+confirmation, func(t *testing.T) {
				p := &extensionCLIProbe{fileData: []byte("download contents")}
				path := extensionCLIFixture(t, p)
				command := append([]string{}, args...)
				command = append(command, "--server", "cli-server", "--request-id", "stable-request")
				if confirmation != "missing" {
					command = append(command, confirmation)
				}
				output, err := runManagement(t, path, command...)
				p.mu.Lock()
				mutations := append([]proto.Message{}, p.mutations...)
				p.mu.Unlock()
				if confirmation != "--yes" {
					if err == nil || !strings.Contains(err.Error(), "--yes") || output != "" || len(mutations) != 0 {
						t.Fatalf("unconfirmed mutation: output=%q err=%v mutations=%v", output, err, mutations)
					}
					return
				}
				if err != nil || output == "" || len(mutations) != 1 {
					t.Fatalf("confirmed action: output=%q err=%v mutations=%v", output, err, mutations)
				}
				if request, ok := mutations[0].(interface{ GetRequestId() string }); ok && request.GetRequestId() != "stable-request" {
					t.Fatalf("request ID changed: %v", mutations[0])
				}
			})
		}
	}
}

func TestRegionPlanUsesExplicitConfigurationAndObservedRevision(t *testing.T) {
	file := filepath.Join(t.TempDir(), "region.json")
	if err := os.WriteFile(file, []byte(`{"country":"US","timezone":"America/New_York","locale":"en_US.UTF-8","languages":["en_US"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"", "reviewed-v7"} {
		p := &extensionCLIProbe{}
		path := extensionCLIFixture(t, p)
		args := []string{"region", "plan", "--file", file, "--server", "cli-server", "--request-id", "plan-request"}
		if revision != "" {
			args = append(args, "--expected-revision", revision)
		}
		output, err := runManagement(t, path, args...)
		if err != nil || !strings.Contains(output, "plan_cli") {
			t.Fatalf("plan: %s %v", output, err)
		}
		p.mu.Lock()
		requests := append([]proto.Message{}, p.requests...)
		p.mu.Unlock()
		plan, ok := requests[len(requests)-1].(*epb.RegionPlanRequest)
		if !ok || plan.ServerId != "srv_cli" || plan.RequestId != "plan-request" || plan.Config.Country != "US" || plan.Config.Timezone != "America/New_York" || plan.Config.Dns != nil {
			t.Fatalf("unexpected plan request: %v", requests)
		}
		if revision == "" && (len(requests) != 2 || plan.ExpectedRevision != "host-v1") {
			t.Fatalf("missing fresh revision: %v", requests)
		}
		if revision != "" && (len(requests) != 1 || plan.ExpectedRevision != revision) {
			t.Fatalf("explicit revision replaced: %v", requests)
		}
	}
}

func TestBrowserProfileUpdateAndBootPreserveUnchangedFields(t *testing.T) {
	for _, tc := range []struct {
		args                              []string
		wantName                          string
		wantBoot, wantRegion, wantReplace bool
	}{
		{args: []string{"update", "profile_cli", "--name", "Renamed"}, wantName: "Renamed", wantRegion: true},
		{args: []string{"update", "profile_cli", "--inherit-region"}, wantName: "Private profile"},
		{args: []string{"boot", "profile_cli", "--replace-boot"}, wantName: "Private profile", wantBoot: true, wantRegion: true, wantReplace: true},
		{args: []string{"create", "new_profile", "--name", "New"}, wantName: "New"},
	} {
		p := &extensionCLIProbe{}
		path := extensionCLIFixture(t, p)
		args := append([]string{"browser", "profile"}, tc.args...)
		args = append(args, "--yes", "--request-id", "profile-request")
		if _, err := runManagement(t, path, args...); err != nil {
			t.Fatal(err)
		}
		p.mu.Lock()
		request := p.mutations[0].(*epb.PutBrowserProfileRequest)
		p.mu.Unlock()
		if request.Profile.Name != tc.wantName || request.Profile.BootStart != tc.wantBoot || (request.Profile.Region != nil) != tc.wantRegion || request.ReplaceBootProfile != tc.wantReplace || request.Profile.Revision != "" || request.Profile.State != "" {
			t.Fatalf("unexpected profile replacement: %v", request)
		}
		if tc.args[0] == "create" {
			if request.ExpectedRevision != "" {
				t.Fatalf("create got old revision: %v", request)
			}
		} else if request.ExpectedRevision != "profile-v1" {
			t.Fatalf("missing observed profile revision: %v", request)
		}
	}
}

func TestExtensionDiagnosticsHaveNoImplicitExternalProviders(t *testing.T) {
	for _, args := range [][]string{{"region", "diagnose"}, {"browser", "diagnose", "profile_cli"}, {"browser", "diagnose", "profile_cli", "--allow-webrtc"}} {
		p := &extensionCLIProbe{}
		path := extensionCLIFixture(t, p)
		output, err := runManagement(t, path, args...)
		if err != nil || !strings.Contains(output, "vantage") {
			t.Fatalf("diagnose: %s %v", output, err)
		}
		p.mu.Lock()
		request := p.requests[0].(*epb.DiagnosticRequest)
		p.mu.Unlock()
		if request.AllowExternal || len(request.Providers) != 0 || request.ServerId != "srv_cli" || request.AllowWebrtc != (len(args) == 4) {
			t.Fatalf("implicit network probe or incorrect consent: %v", request)
		}
		if args[0] == "browser" && request.ProfileId != "profile_cli" {
			t.Fatalf("wrong Chrome vantage: %v", request)
		}
	}
}

func TestBrowserViewerUsesLocalTypedServiceAndPrivateStdout(t *testing.T) {
	p := &extensionCLIProbe{}
	path := extensionCLIFixture(t, p)
	var output, diagnostics bytes.Buffer
	root := newRoot(nil, &output, &diagnostics)
	root.SetArgs([]string{"--config", path, "--json", "--server", "cli-server", "browser", "open", "profile_cli"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "private-bootstrap-test") || strings.Contains(diagnostics.String(), "private-bootstrap-test") {
		t.Fatalf("viewer URL not confined to the requested result: stdout=%q stderr=%q", output.String(), diagnostics.String())
	}
	p.mu.Lock()
	request, ok := p.requests[0].(*epb.OpenBrowserViewerRequest)
	p.mu.Unlock()
	if !ok || request.ServerId != "srv_cli" || request.ProfileId != "profile_cli" || !request.ReadOnly {
		t.Fatalf("incorrect local viewer request: %v", request)
	}
	if _, err := runManagement(t, path, "browser", "close-viewer", "viewer_cli"); err != nil {
		t.Fatal(err)
	}
	if _, err := runManagement(t, path, "browser", "close-viewer", "viewer_cli", "--server", "cli-server"); err == nil || !strings.Contains(err.Error(), "omit --server") {
		t.Fatalf("local close accepted remote routing: %v", err)
	}
}

func TestBrowserFileTransfersAreBoundedAndVerified(t *testing.T) {
	data := bytes.Repeat([]byte("private file data\n"), 9000)
	p := &extensionCLIProbe{fileData: data}
	path := extensionCLIFixture(t, p)
	dir := t.TempDir()
	local, target := filepath.Join(dir, "local.bin"), filepath.Join(dir, "retrieved.bin")
	if err := os.WriteFile(local, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runManagement(t, path, "browser", "files", "upload", "profile_cli", local, "--name", "shared.bin", "--yes", "--request-id", "upload-request"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	chunks := append([]*epb.BrowserFileChunk{}, p.uploadChunks...)
	p.mu.Unlock()
	if len(chunks) < 4 || chunks[0].RequestId != "upload-request" || chunks[0].TotalSize != int64(len(data)) || !chunks[len(chunks)-1].Finish {
		t.Fatalf("incorrect upload framing: %v", chunks)
	}
	for _, chunk := range chunks {
		if len(chunk.Data) > browserFileChunkSize {
			t.Fatal("unbounded upload chunk")
		}
	}
	if _, err := runManagement(t, path, "browser", "files", "download", "profile_cli", "shared.bin", "--output", target, "--yes"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("download contents: len=%d err=%v", len(got), err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("download permissions: %v %v", info, err)
	}
	p.corruptDownload = true
	if _, err := runManagement(t, path, "browser", "files", "download", "profile_cli", "shared.bin", "--output", target, "--overwrite", "--yes"); err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("corrupt file published: %v", err)
	}
	got, err = os.ReadFile(target)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("failed overwrite damaged the existing destination")
	}
	partials, err := filepath.Glob(filepath.Join(dir, ".shiki-browser-download-*"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("private partial files leaked: %v %v", partials, err)
	}
}

func TestRegionApplyFailureProvidesPendingOperationRecovery(t *testing.T) {
	p := &extensionCLIProbe{applyErr: connect.NewError(connect.CodeDeadlineExceeded, "helper response was lost")}
	path := extensionCLIFixture(t, p)
	output, err := runManagement(t, path, "region", "apply", "plan_cli", "--expected-revision", "host-v1", "--yes")
	if output != "" || err == nil || !strings.Contains(err.Error(), "pending_operation_id") || !strings.Contains(err.Error(), "confirm_before") || !strings.Contains(err.Error(), "srv_cli") || connect.CodeOf(err) != connect.CodeDeadlineExceeded {
		t.Fatalf("apply response-loss recovery was not actionable: output=%q err=%v", output, err)
	}
	output, err = runManagement(t, path, "region", "inspect")
	status := new(epb.RegionStatus)
	decodeErr := protojson.Unmarshal([]byte(output), status)
	if err != nil || decodeErr != nil || status.PendingOperationId != "pending_cli" || status.ConfirmBefore != 1900000000 {
		t.Fatalf("inspect omitted pending recovery identity: output=%q err=%v", output, err)
	}
}

func TestExtensionExplicitDiagnosticProvidersReachTypedRequest(t *testing.T) {
	p := &extensionCLIProbe{}
	path := extensionCLIFixture(t, p)
	file := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(file, []byte(`{"providers":[{"id":"mine","kind":"ip-echo","url":"https://diagnostic.example/ip","ip_family":"ipv6"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runManagement(t, path, "browser", "diagnose", "profile_cli", "--allow-external", "--providers", file, "--allow-webrtc"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	request := p.requests[0].(*epb.DiagnosticRequest)
	p.mu.Unlock()
	if !request.AllowExternal || !request.AllowWebrtc || request.ServerId != "srv_cli" || request.ProfileId != "profile_cli" || len(request.Providers) != 1 || request.Providers[0].Id != "mine" || request.Providers[0].IpFamily != "ipv6" {
		t.Fatalf("explicit diagnostic consent or provider was changed: %v", request)
	}
}

func TestRegionRollbackConflictReturnsFailureWithEvidence(t *testing.T) {
	p := &extensionCLIProbe{rollbackState: "rollback_conflict"}
	path := extensionCLIFixture(t, p)
	output, err := runManagement(t, path, "region", "rollback", "operation_cli", "--yes")
	if err == nil {
		t.Fatal("CLI reported successful rollback despite external configuration conflict")
	}
	result := new(epb.RegionOperation)
	if decodeErr := protojson.Unmarshal([]byte(output), result); decodeErr != nil || result.Id != "operation_cli" || result.State != "rollback_conflict" {
		t.Fatalf("rollback failure lost its structured recovery evidence: %q, %v", output, decodeErr)
	}
}

func TestBrowserLaunchModeCLIAndOldAgentGate(t *testing.T) {
	for _, old := range []bool{false, true} {
		for _, diagnostics := range []bool{false, true} {
			p := &extensionCLIProbe{oldAgent: old}
			path := extensionCLIFixture(t, p)
			args := []string{"browser", "start", "profile_cli", "--yes", "--replace-running"}
			if diagnostics {
				args = append(args, "--diagnostics")
			}
			output, err := runManagement(t, path, args...)
			p.mu.Lock()
			mutations := append([]proto.Message(nil), p.mutations...)
			p.mu.Unlock()
			if old {
				if err == nil || !strings.Contains(err.Error(), "browser.launch-mode.v1") || len(mutations) != 0 {
					t.Fatalf("old agent received start: %v %v", err, mutations)
				}
				continue
			}
			if err != nil || len(mutations) != 1 {
				t.Fatal(err, mutations)
			}
			request := mutations[0].(*epb.BrowserActionRequest)
			if request.Diagnostics != diagnostics || !request.ReplaceRunning || !request.Confirm {
				t.Fatal(request)
			}
			mode := "browse"
			if diagnostics {
				mode = "diagnostic"
			}
			if !strings.Contains(output, mode) {
				t.Fatal("CLI omitted actual launch mode", output)
			}
		}
	}
	p := &extensionCLIProbe{}
	path := extensionCLIFixture(t, p)
	if _, err := runManagement(t, path, "browser", "stop", "profile_cli", "--yes", "--diagnostics"); err == nil {
		t.Fatal("stop accepted diagnostic launch option")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.mutations) != 0 {
		t.Fatal("invalid stop reached mutation")
	}
}
