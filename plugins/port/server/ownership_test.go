package server

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type rootsCore struct {
	rpc.CoreAPIClient
	roots  []*pb.Session
	calls  int
	change bool
}

func (c *rootsCore) ListSessions(context.Context, *ppb.Scope) (*pb.ListSessionsResponse, error) {
	c.calls++
	out := &pb.ListSessionsResponse{}
	for _, s := range c.roots {
		v := proto.Clone(s).(*pb.Session)
		if c.change && c.calls%2 == 0 {
			v.TmuxPane = "%replaced"
		}
		out.Sessions = append(out.Sessions, v)
	}
	return out, nil
}
func (c *rootsCore) GetSession(_ context.Context, s *ppb.Scope) (*pb.Session, error) {
	for _, v := range c.roots {
		if v.Id == s.SessionId {
			return proto.Clone(v).(*pb.Session), nil
		}
	}
	return nil, connect.NewError(connect.CodeNotFound, "missing")
}
func root(pid int32) *pb.Session {
	return &pb.Session{Id: "sess_owned", ProjectId: "proj_owned", ClaudePid: pid, TmuxWindow: "@verified", TmuxPane: "%verified", Lifecycle: "ready"}
}

// Keep real OS enumeration and process identity checks, while restricting this
// authorization fixture to the listener it owns. Unrelated parallel tests may
// exit between a system scan and ancestry checks; that is a valid conservative
// scan failure, not the unknown-owner decision this fixture exercises.
type ownedEndpointScanner struct {
	address string
	port    int32
}

func (s ownedEndpointScanner) Scan(ctx context.Context) ([]*pb.OpenPort, error) {
	ports, err := (SystemScanner{}).Scan(ctx)
	if err != nil {
		return nil, err
	}
	var selected []*pb.OpenPort
	for _, port := range ports {
		if port.BindAddress == s.address && port.Port == s.port {
			selected = append(selected, port)
		}
	}
	return selected, nil
}

func TestRealOwnershipAuthorizationAndNoDial(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	target, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	port := int32(target.Addr().(*net.TCPAddr).Port)
	core := &rootsCore{roots: []*pb.Session{root(int32(os.Getpid()))}}
	w := NewWithScanner(ownedEndpointScanner{address: "127.0.0.1", port: port})
	cfg, _ := structpb.NewStruct(map[string]any{"allow_ports": []any{float64(port), 9527.0}, "allow_protected": true})
	if e = w.Init(ctx, sdk.InitContext{Side: "server", Core: core, StateDir: t.TempDir(), Config: cfg}); e != nil {
		t.Fatal(e)
	}
	if e = w.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer w.Stop(context.Background())
	list, e := w.ListPorts(ctx, &pb.ListPortsRequest{SessionId: "sess_owned"})
	if e != nil {
		t.Fatal(e)
	}
	var observed *pb.OpenPort
	for _, p := range list.Ports {
		if p.Port == port && p.BindAddress == "127.0.0.1" {
			observed = p
		}
	}
	if observed == nil || observed.SessionId != "sess_owned" || len(observed.SourceIdentity) != 64 {
		t.Fatal("owned process not verified", observed)
	}
	request := &pb.CheckPortRequest{RemoteAddress: "127.0.0.1", Port: port, SessionId: observed.SessionId, Automatic: true, SourceIdentity: observed.SourceIdentity}
	if _, e = w.CheckPort(ctx, request); e != nil {
		t.Fatal(e)
	}
	target.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Millisecond))
	conn, e := target.Accept()
	if conn != nil {
		conn.Close()
		t.Fatal("authorization dialed target")
	}
	if e == nil {
		t.Fatal("expected no target traffic")
	}
	for _, changed := range []func(*pb.CheckPortRequest){func(r *pb.CheckPortRequest) { r.SourceIdentity = strings.Repeat("0", 64) }, func(r *pb.CheckPortRequest) { r.SessionId = "sess_other" }, func(r *pb.CheckPortRequest) { r.Port = 9527 }, func(r *pb.CheckPortRequest) { r.Port = 1234 }} {
		r := proto.Clone(request).(*pb.CheckPortRequest)
		changed(r)
		if _, e = w.CheckPort(ctx, r); connect.CodeOf(e) != connect.CodePermissionDenied {
			t.Fatal("unsafe automatic target allowed", r, e)
		}
	}
	core.roots = nil
	if _, e = w.CheckPort(ctx, request); connect.CodeOf(e) != connect.CodePermissionDenied {
		t.Fatal("unknown owner allowed", e)
	}
	core.roots = []*pb.Session{root(int32(os.Getpid()))}
	core.calls = 0
	core.change = true
	ports, e := w.scanOwned(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range ports {
		if p.Port == port && p.SessionId != "" {
			t.Fatal("changed pane retained ownership", p)
		}
	}
}

type fixedScanner struct {
	ports []*pb.OpenPort
	err   error
}

func (s fixedScanner) Scan(context.Context) ([]*pb.OpenPort, error) { return s.ports, s.err }

func TestAutomaticAuthorizationRejectsDisappearedListenerOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	port := int32(target.Addr().(*net.TCPAddr).Port)
	child := exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := int32(child.Process.Pid)
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectProcess(ctx, pid); err == nil {
		t.Fatal("fixture process identity still exists")
	}
	// Model an authentic scanner snapshot whose listener owner exited before
	// ancestry verification. The unresolved PID must not become authorization.
	w := NewWithScanner(fixedScanner{ports: []*pb.OpenPort{{Port: port, BindAddress: "127.0.0.1", Protocol: "tcp", Pid: pid}}})
	cfg, err := structpb.NewStruct(map[string]any{"allow_ports": []any{float64(port)}})
	if err != nil {
		t.Fatal(err)
	}
	core := &rootsCore{roots: []*pb.Session{root(int32(os.Getpid()))}}
	if err := w.Init(ctx, sdk.InitContext{Side: "server", Core: core, StateDir: t.TempDir(), Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop(context.Background())
	request := &pb.CheckPortRequest{RemoteAddress: "127.0.0.1", Port: port, SessionId: "sess_owned", Automatic: true, SourceIdentity: strings.Repeat("0", 64)}
	if result, err := w.CheckPort(ctx, request); result != nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("unverifiable process was not conservatively rejected", result, err)
	}
	target.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Millisecond))
	conn, err := target.Accept()
	if conn != nil {
		conn.Close()
		t.Fatal("failed ownership scan dialed the target")
	}
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatal("no-dial observation did not reach its deadline", err)
	}
}

func TestUnknownAmbiguousAndFailedOwnershipScan(t *testing.T) {
	core := &rootsCore{roots: []*pb.Session{root(int32(os.Getpid()))}}
	w := NewWithScanner(fixedScanner{ports: []*pb.OpenPort{{Port: 3000, Pid: int32(os.Getppid()), BindAddress: "127.0.0.1", SessionId: "forged", SourceIdentity: "forged"}}})
	w.core = core
	ports, e := w.scanOwned(t.Context())
	if e != nil || ports[0].SessionId != "" || ports[0].SourceIdentity != "" {
		t.Fatal("unrelated process attributed", ports, e)
	}
	w.scanner = fixedScanner{ports: []*pb.OpenPort{{Port: 3000, Pid: int32(os.Getpid()), BindAddress: "127.0.0.1"}}}
	core.roots = append(core.roots, proto.Clone(core.roots[0]).(*pb.Session))
	ports, e = w.scanOwned(t.Context())
	if e != nil || ports[0].SessionId != "" {
		t.Fatal("ambiguous roots attributed", ports, e)
	}
	w.scanner = fixedScanner{err: errors.New("scan failed")}
	if _, e = w.scanOwned(t.Context()); e == nil {
		t.Fatal("scan failure treated as empty")
	}
}
