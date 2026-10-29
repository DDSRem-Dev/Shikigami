package client

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	portplugin "github.com/DDSRem-Dev/Shikigami/plugins/port/config"
	"github.com/DDSRem-Dev/Shikigami/plugins/port/tunnel"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type Worker struct {
	mu          sync.Mutex
	core        sdk.CoreAPI
	config      portplugin.Config
	lifetime    context.Context
	cancel      context.CancelFunc
	closed      bool
	tasks       map[string]*task
	connections int
}
type task struct {
	state    *ppb.ForwardState
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	wg       sync.WaitGroup
}

func New() *Worker { return &Worker{tasks: map[string]*task{}} }
func (w *Worker) Info() *ppb.PluginInfo {
	return &ppb.PluginInfo{Name: "port", Version: buildinfo.Version, Side: "client", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", sdk.PortWorkerCapability, sdk.PortAutoCapability}}}
}
func ValidateConfig(config *structpb.Struct) error {
	_, err := portplugin.Parse(config, "client")
	return err
}
func (w *Worker) Init(_ context.Context, in sdk.InitContext) error {
	if in.Core == nil || in.Side != "client" || !filepath.IsAbs(in.StateDir) {
		return errors.New("invalid client port initialization")
	}
	config, err := portplugin.Parse(in.Config, "client")
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.core != nil {
		return errors.New("port already initialized")
	}
	w.core = in.Core
	w.config = config
	return nil
}
func (w *Worker) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.core == nil || w.closed {
		return errors.New("port not initialized")
	}
	if w.lifetime != nil && w.lifetime.Err() != nil {
		return connect.NewError(connect.CodeFailedPrecondition, "PORT_LIFETIME_ENDED")
	}
	if w.lifetime == nil {
		w.lifetime, w.cancel = context.WithCancel(ctx)
	}
	return nil
}
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	w.closed = true
	if w.cancel != nil {
		w.cancel()
	}
	var done []<-chan struct{}
	for _, t := range w.tasks {
		t.state.State = "stopped"
		t.state.ReasonCode = ""
		if t.cancel != nil {
			t.cancel()
		}
		if t.listener != nil {
			t.listener.Close()
		}
		done = append(done, t.done)
	}
	w.mu.Unlock()
	for _, d := range done {
		select {
		case <-d:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (w *Worker) Health(context.Context) (*pb.HealthStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	status := pb.HealthStatus_HEALTHY
	if w.closed || w.lifetime == nil || w.lifetime.Err() != nil {
		status = pb.HealthStatus_UNHEALTHY
	}
	return &pb.HealthStatus{Status: status}, nil
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func normalize(in *ppb.ForwardSpec) (*ppb.ForwardSpec, error) {
	if in == nil || !identifier.MatchString(in.Id) || !identifier.MatchString(in.ServerId) || in.RemotePort < 1 || in.RemotePort > 65535 || in.LocalPort > 65535 {
		return nil, connect.NewError(connect.CodeInvalidArgument, "INVALID_FORWARD_SPEC")
	}
	s := proto.Clone(in).(*ppb.ForwardSpec)
	if s.RemoteAddress == "" {
		s.RemoteAddress = "127.0.0.1"
	}
	if s.LocalAddress == "" {
		s.LocalAddress = "127.0.0.1"
	}
	local, err := portplugin.Loopback(s.LocalAddress)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "LOCAL_LOOPBACK_REQUIRED")
	}
	remote, err := portplugin.Loopback(s.RemoteAddress)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "REMOTE_LOOPBACK_REQUIRED")
	}
	s.LocalAddress = local.String()
	s.RemoteAddress = remote.String()
	return s, nil
}
func (w *Worker) StartForward(ctx context.Context, in *ppb.ForwardSpec) (*ppb.ForwardState, error) {
	if in != nil && in.LocalAddress == "" {
		w.mu.Lock()
		bindAddress := w.config.BindAddress
		w.mu.Unlock()
		in = proto.Clone(in).(*ppb.ForwardSpec)
		in.LocalAddress = bindAddress
	}
	spec, err := normalize(in)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.lifetime == nil || w.lifetime.Err() != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "PORT_NOT_RUNNING")
	}
	if spec.Automatic && (!w.config.AutomaticAllowed(spec.RemotePort) || spec.SessionId == "" || len(spec.SourceIdentity) != 64 || spec.LocalAddress != w.config.BindAddress) {
		return nil, connect.NewError(connect.CodePermissionDenied, "AUTOMATIC_POLICY_CHANGED")
	}
	if old := w.tasks[spec.Id]; old != nil {
		select {
		case <-old.done:
		default:
			if proto.Equal(old.state.Spec, spec) {
				return proto.Clone(old.state).(*ppb.ForwardState), nil
			}
			return nil, connect.NewError(connect.CodeAborted, "FORWARD_CONFIG_CHANGED")
		}
	} else if len(w.tasks) >= 256 {
		// Stopped runtime snapshots do not consume an active listener quota
		// forever. Durable desired configuration belongs to the agent.
		for id, old := range w.tasks {
			select {
			case <-old.done:
				delete(w.tasks, id)
			default:
			}
		}
		if len(w.tasks) >= 256 {
			return nil, connect.NewError(connect.CodeResourceExhausted, "FORWARD_LIMIT")
		}
	}
	t := &task{state: &ppb.ForwardState{Spec: spec, State: "stopped"}, done: make(chan struct{})}
	if !spec.Enabled {
		close(t.done)
		w.tasks[spec.Id] = t
		return proto.Clone(t.state).(*ppb.ForwardState), nil
	}
	network := "tcp4"
	if spec.LocalAddress == "::1" {
		network = "tcp6"
	}
	listen := net.ListenConfig{}
	listener, err := listen.Listen(ctx, network, net.JoinHostPort(spec.LocalAddress, strconv.Itoa(int(spec.LocalPort))))
	if err != nil && w.config.Fallback && spec.LocalPort != 0 && errors.Is(err, syscall.EADDRINUSE) {
		listener, err = listen.Listen(ctx, network, net.JoinHostPort(spec.LocalAddress, "0"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "LOCAL_PORT_BIND_FAILED")
	}
	if err = ctx.Err(); err != nil {
		listener.Close()
		return nil, err
	}
	t.listener = listener
	t.ctx, t.cancel = context.WithCancel(w.lifetime)
	t.state.BoundPort = uint32(listener.Addr().(*net.TCPAddr).Port)
	t.state.State = "listening"
	w.tasks[spec.Id] = t
	t.wg.Add(1)
	go w.accept(t)
	go func() {
		t.wg.Wait()
		w.mu.Lock()
		if t.state.State != "offline" && t.state.State != "auth_required" && t.state.State != "error" {
			t.state.State = "stopped"
		}
		t.state.BoundPort = 0
		w.mu.Unlock()
		close(t.done)
	}()
	return proto.Clone(t.state).(*ppb.ForwardState), nil
}
func (w *Worker) StopForward(ctx context.Context, r *ppb.ForwardID) error {
	if r == nil || !identifier.MatchString(r.Id) {
		return connect.NewError(connect.CodeInvalidArgument, "FORWARD_ID_REQUIRED")
	}
	w.mu.Lock()
	t := w.tasks[r.Id]
	if t == nil {
		w.mu.Unlock()
		return nil
	}
	t.state.State = "stopped"
	t.state.ReasonCode = ""
	if t.cancel != nil {
		t.cancel()
	}
	if t.listener != nil {
		t.listener.Close()
	}
	w.mu.Unlock()
	select {
	case <-t.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (w *Worker) ListForwards(ctx context.Context) (*ppb.ForwardList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := &ppb.ForwardList{}
	for _, t := range w.tasks {
		out.Forwards = append(out.Forwards, proto.Clone(t.state).(*ppb.ForwardState))
	}
	sort.Slice(out.Forwards, func(i, j int) bool { return out.Forwards[i].Spec.Id < out.Forwards[j].Spec.Id })
	return out, nil
}
func (w *Worker) accept(t *task) {
	defer t.wg.Done()
	stop := context.AfterFunc(t.ctx, func() { t.listener.Close() })
	defer stop()
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			return
		}
		w.mu.Lock()
		if w.connections >= w.config.MaxConnections || t.ctx.Err() != nil {
			w.mu.Unlock()
			conn.Close()
			continue
		}
		w.connections++
		t.state.Connections++
		t.wg.Add(1)
		w.mu.Unlock()
		go func() {
			defer t.wg.Done()
			defer conn.Close()
			defer func() { w.mu.Lock(); w.connections--; t.state.Connections--; w.mu.Unlock() }()
			w.forward(t, conn.(*net.TCPConn))
		}()
	}
}

type remoteStream struct {
	raw rpc.CoreAPIRemoteForwardPortClientStream
}

func (s remoteStream) Send(p *pb.PortData) error {
	return s.raw.Send(&ppb.RemotePortFrame{Payload: &ppb.RemotePortFrame_Data{Data: p}})
}
func (s remoteStream) Receive() (*pb.PortData, error) { return s.raw.Receive() }
func (w *Worker) forward(t *task, conn *net.TCPConn) {
	ctx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	handshake := time.AfterFunc(w.config.DialTimeout, cancel)
	defer handshake.Stop()
	raw, err := w.core.RemoteForwardPort(ctx)
	if err != nil {
		w.handshakeFailed(t, err)
		return
	}
	defer raw.Close()
	spec := t.state.Spec
	id := rand.Text()
	err = raw.Send(&ppb.RemotePortFrame{Payload: &ppb.RemotePortFrame_Open{Open: &ppb.RemotePortOpen{Scope: &ppb.Scope{ServerId: spec.ServerId, SessionId: spec.SessionId}, Connect: &pb.PortData{Type: pb.PortData_CONNECT, Port: int32(spec.RemotePort), RemoteAddress: spec.RemoteAddress, ConnectionId: id, SessionId: spec.SessionId, Automatic: spec.Automatic, SourceIdentity: spec.SourceIdentity}}}})
	if err != nil && !errors.Is(err, io.EOF) {
		w.handshakeFailed(t, err)
		return
	}
	// An early rejection may close Send before its final status is read. The
	// existing handshake deadline bounds Receive; preserve that final reason.
	sendErr := err
	ack, err := raw.Receive()
	if err != nil {
		w.handshakeFailed(t, err)
		return
	}
	if sendErr != nil {
		w.failed(t, sendErr)
		return
	}
	if err = tunnel.ConnectedFrame(ack, id); err != nil {
		w.failed(t, err)
		return
	}
	if !handshake.Stop() && ctx.Err() != nil {
		w.failed(t, ctx.Err())
		return
	}
	w.mu.Lock()
	if t.ctx.Err() != nil {
		w.mu.Unlock()
		return
	}
	t.state.State = "listening"
	t.state.ReasonCode = ""
	w.mu.Unlock()
	err = tunnel.Bridge(ctx, conn, remoteStream{raw}, tunnel.Options{ID: id, Idle: w.config.IdleTimeout, CloseSend: raw.CloseSend, CloseStream: func() error { cancel(); return raw.Close() }, Count: func(n int) { w.mu.Lock(); t.state.BytesTransferred += uint64(n); w.mu.Unlock() }})
	if err != nil && t.ctx.Err() == nil {
		// A local application's reset or one idle TCP session does not imply
		// loss of the authenticated remote route. Only that connection ends.
		var localSocketError *net.OpError
		if errors.As(err, &localSocketError) || connect.CodeOf(err) == connect.CodeDeadlineExceeded {
			return
		}
		w.failed(t, err)
	}
}
func (w *Worker) handshakeFailed(t *task, err error) {
	// Resource admission rejects this new stream, not the authenticated route
	// or its admitted connections. Proxy boundaries deliberately sanitize the
	// message, so use the preserved code only before CONNECTED. Established
	// stream failures still take the separate task-level path below.
	if connect.CodeOf(err) == connect.CodeResourceExhausted {
		return
	}
	w.failed(t, err)
}
func (w *Worker) failed(t *task, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t.ctx.Err() != nil {
		return
	}
	t.cancel()
	t.listener.Close()
	t.state.BoundPort = 0
	switch connect.CodeOf(err) {
	case connect.CodeUnauthenticated:
		t.state.State = "auth_required"
		t.state.ReasonCode = "REMOTE_AUTH_REQUIRED"
	case connect.CodePermissionDenied:
		t.state.State = "error"
		t.state.ReasonCode = "REMOTE_TARGET_DENIED"
	case connect.CodeUnavailable:
		t.state.State = "offline"
		t.state.ReasonCode = "REMOTE_UNAVAILABLE"
	default:
		t.state.State = "error"
		t.state.ReasonCode = "FORWARD_STREAM_FAILED"
	}
}
