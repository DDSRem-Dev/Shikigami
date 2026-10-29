package version_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

var fixtureSide = flag.String("side", "client", "test-only go-plugin side")

type fixtureConfiguration struct {
	InvalidRequiredAgreement                       bool
	Migrate                                        bool
	Mode                                           string
	Offer                                          *pb.ProtocolOffer
	InitOffer                                      *pb.ProtocolOffer
	Address, Ready, Marker, Cert, Key, Auth, Build string
	Unimplemented                                  bool
	InvalidAgreement                               bool
}

func (c fixtureConfiguration) mark(value string) error {
	file, err := os.OpenFile(c.Marker, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(value + "\n")
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Synthetic peers exercise real transports and production runtime/SDK code.
// They are explicitly not historical Shikigami releases.
func TestVersionProcessFixture(t *testing.T) {
	path := os.Getenv("SHIKI_VERSION_FIXTURE")
	if path == "" {
		return
	}
	cfg := readFixture(t, path)
	ctx, cancel := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if cfg.Mode == "plugin" {
		if err := cfg.mark("pid:" + strconv.Itoa(os.Getpid())); err != nil {
			t.Fatal(err)
		}
		if err := sdk.Serve(*fixtureSide, &versionPlugin{cfg: cfg}); err != nil {
			t.Fatal(err)
		}
		return
	}
	if cfg.Mode == "state" {
		options := state.Options{DisableRecovery: true}
		if cfg.Migrate {
			options.Migrations = map[int]func(*state.Snapshot) error{0: func(s *state.Snapshot) error { s.SchemaVersion = 1; return nil }}
		}
		store, err := state.Open(cfg.Address, options)
		result := "opened"
		if errors.Is(err, state.ErrFutureSchema) {
			result = "future_schema"
		} else if err != nil {
			t.Fatal(err)
		}
		if store != nil {
			defer store.Close()
			if cfg.Migrate {
				if err := store.Update(func(s *state.Snapshot) error { return s.Put("fixture", "new", map[string]string{"value": "committed"}) }); err != nil {
					t.Fatal(err)
				}
			}
		}
		writeJSON(t, cfg.Ready, struct{ Result string }{result})
		return
	}
	server := connect.NewServer()
	var middleware []func(http.Handler) http.Handler
	var listener net.Listener
	var err error
	if cfg.Mode == "agent" {
		listener, err = connection.ListenUnix(cfg.Address, uint32(os.Getuid()))
		arpc.RegisterAgentHandler(server, &versionAgent{cfg: cfg})
	} else if cfg.Mode == "daemon" {
		authentication, openErr := auth.Open(cfg.Auth, "version-test")
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer authentication.Close()
		server = connect.NewServer(authentication.Interceptor())
		middleware = append(middleware, authentication.HTTPMiddleware)
		id, idErr := authentication.DaemonID()
		if idErr != nil {
			t.Fatal(idErr)
		}
		rpc.RegisterShikiDaemonHandler(server, &versionDaemon{cfg: cfg, id: id})
		if cfg.Address == "" {
			cfg.Address = "127.0.0.1:0"
		}
		listener, err = net.Listen("tcp", cfg.Address)
	} else {
		t.Fatal("unknown fixture mode")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	httpServer := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server, middleware...))
	done := make(chan error, 1)
	go func() {
		if cfg.Mode == "daemon" {
			done <- connection.ServeTLS(httpServer, listener, cfg.Cert, cfg.Key)
		} else {
			done <- httpServer.Serve(listener)
		}
	}()
	writeJSON(t, cfg.Ready, struct{ Address string }{listener.Addr().String()})
	select {
	case <-ctx.Done():
		httpServer.Close()
		<-done
	case err := <-done:
		t.Fatal(err)
	}
}

type versionAgent struct {
	arpc.UnimplementedAgentHandler
	cfg fixtureConfiguration
}

func (a *versionAgent) Handshake(_ context.Context, in *apb.HandshakeRequest) (*apb.HandshakeResponse, error) {
	if err := a.cfg.mark("Handshake"); err != nil {
		return nil, err
	}
	agreement, err := version.Negotiate(a.cfg.Offer, in.Protocol, a.cfg.Build)
	if err != nil {
		return nil, err
	}
	if a.cfg.InvalidAgreement {
		agreement.Major = 2
	}
	return &apb.HandshakeResponse{Protocol: agreement, InstanceId: "version-instance", FrontendId: "version-frontend"}, nil
}
func (a *versionAgent) GetStatus(context.Context, *pb.Empty) (*apb.AgentStatus, error) {
	if err := a.cfg.mark("GetStatus"); err != nil {
		return nil, err
	}
	return &apb.AgentStatus{InstanceId: "version-instance", State: "ready"}, nil
}
func (a *versionAgent) ReleaseFrontend(context.Context, *pb.Empty) (*pb.Empty, error) {
	return &pb.Empty{}, a.cfg.mark("ReleaseFrontend")
}
func (a *versionAgent) Shutdown(context.Context, *apb.ShutdownRequest) (*apb.AgentStatus, error) {
	return &apb.AgentStatus{}, a.cfg.mark("Shutdown")
}

type versionDaemon struct {
	rpc.UnimplementedShikiDaemonHandler
	cfg fixtureConfiguration
	id  string
}

func (d *versionDaemon) NegotiateProtocol(_ context.Context, offer *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	if err := d.cfg.mark("NegotiateProtocol"); err != nil {
		return nil, err
	}
	if d.cfg.Unimplemented {
		return nil, connect.NewError(connect.CodeUnimplemented, "synthetic peer has no negotiation")
	}
	agreement, err := version.Negotiate(d.cfg.Offer, offer, d.cfg.Build)
	if err != nil {
		return nil, err
	}
	if d.cfg.InvalidAgreement {
		agreement.Major = 2
	}
	if d.cfg.InvalidRequiredAgreement && len(offer.RequiredCapabilities) > 0 {
		agreement.Capabilities = nil
	}
	agreement.DaemonId = d.id
	agreement.GrantedPermissions = []string{"system.info", "system.stats", "project.list", "session.attach"}
	return agreement, nil
}
func (d *versionDaemon) GetSystemInfo(context.Context, *pb.Empty) (*pb.SystemInfo, error) {
	return &pb.SystemInfo{DaemonId: d.id, DaemonVersion: d.cfg.Build}, d.cfg.mark("GetSystemInfo")
}
func (d *versionDaemon) ListProjects(context.Context, *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	return &pb.ListProjectsResponse{}, d.cfg.mark("ListProjects")
}
func (d *versionDaemon) GetStats(context.Context, *pb.StatsRequest) (*pb.StatsSnapshot, error) {
	return &pb.StatsSnapshot{}, d.cfg.mark("GetStats")
}
func (d *versionDaemon) CreateProject(context.Context, *pb.CreateProjectRequest) (*pb.Project, error) {
	return &pb.Project{Id: "proj_forbidden"}, d.cfg.mark("CreateProject")
}
func (d *versionDaemon) TerminalStream(context.Context, rpc.ShikiDaemonTerminalStreamServerStream) error {
	if err := d.cfg.mark("TerminalStream"); err != nil {
		return err
	}
	return connect.NewError(connect.CodeFailedPrecondition, "fixture terminal body must not be reached without negotiated capability")
}

type versionPlugin struct {
	cfg   fixtureConfiguration
	calls int
}

func (p *versionPlugin) Info() *ppb.PluginInfo {
	p.calls++
	offer := p.cfg.Offer
	if p.calls >= 3 && p.cfg.InitOffer != nil {
		offer = p.cfg.InitOffer
	}
	return &ppb.PluginInfo{Name: "sync", Version: p.cfg.Build, Side: "both", Protocol: offer}
}
func (p *versionPlugin) Init(_ context.Context, in sdk.InitContext) error {
	return p.cfg.mark(fmt.Sprintf("Init:%d", in.Protocol.Minor))
}
func (p *versionPlugin) Start(context.Context) error { return p.cfg.mark("Start") }
func (p *versionPlugin) Stop(context.Context) error  { return p.cfg.mark("Stop") }
func (p *versionPlugin) Health(context.Context) (*pb.HealthStatus, error) {
	return &pb.HealthStatus{Status: pb.HealthStatus_HEALTHY}, nil
}
