package server

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	portplugin "github.com/DDSRem-Dev/Shikigami/plugins/port/config"
	"github.com/DDSRem-Dev/Shikigami/plugins/port/tunnel"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type Worker struct {
	mu          sync.Mutex
	config      portplugin.Config
	initialized bool
	lifetime    context.Context
	cancel      context.CancelFunc
	closed      bool
	active      sync.WaitGroup
	operations  [3]int
	stopped     chan struct{}
	scanner     Scanner
	core        sdk.CoreAPI
}

func New() *Worker                     { return NewWithScanner(SystemScanner{}) }
func NewWithScanner(s Scanner) *Worker { return &Worker{scanner: s} }
func (w *Worker) Info() *ppb.PluginInfo {
	return &ppb.PluginInfo{Name: "port", Version: buildinfo.Version, Side: "server", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", sdk.PortCapability, sdk.PortCheckCapability, sdk.PortAutoCapability}}}
}
func ValidateConfig(config *structpb.Struct) error {
	_, err := portplugin.Parse(config, "server")
	return err
}
func (w *Worker) Init(_ context.Context, in sdk.InitContext) error {
	if in.Core == nil || in.Side != "server" || !filepath.IsAbs(in.StateDir) {
		return errors.New("invalid server port initialization")
	}
	config, err := portplugin.Parse(in.Config, "server")
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.initialized {
		return errors.New("port already initialized")
	}
	w.config = config
	w.core = in.Core
	w.initialized = true
	return nil
}
func (w *Worker) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.initialized || w.closed {
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
	if w.stopped == nil {
		w.closed = true
		if w.cancel != nil {
			w.cancel()
		}
		w.stopped = make(chan struct{})
		go func() { w.active.Wait(); close(w.stopped) }()
	}
	done := w.stopped
	w.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

type operationClass uint8

const (
	metadataOperation operationClass = iota
	watchOperation
	connectionOperation
)

func (w *Worker) begin(parent context.Context, class operationClass) (context.Context, func(), error) {
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	w.mu.Lock()
	if w.closed || w.lifetime == nil || w.lifetime.Err() != nil {
		w.mu.Unlock()
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "PORT_NOT_RUNNING")
	}
	maximum, reason := limits.PortMetadataCalls, "PORT_METADATA_LIMIT"
	switch class {
	case watchOperation:
		maximum, reason = limits.PortWatches, "PORT_WATCH_LIMIT"
	case connectionOperation:
		maximum, reason = w.config.MaxConnections, "PORT_CONNECTION_LIMIT"
	}
	if w.operations[class] >= maximum {
		w.mu.Unlock()
		return nil, nil, connect.NewError(connect.CodeResourceExhausted, reason)
	}
	w.operations[class]++
	w.active.Add(1)
	lifetime := w.lifetime
	w.mu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(lifetime, cancel)
	return ctx, func() {
		stop()
		cancel()
		w.mu.Lock()
		w.operations[class]--
		w.mu.Unlock()
		w.active.Done()
	}, nil
}
func (w *Worker) ListPorts(parent context.Context, r *pb.ListPortsRequest) (*pb.ListPortsResponse, error) {
	ctx, done, err := w.begin(parent, metadataOperation)
	if err != nil {
		return nil, err
	}
	defer done()
	ports, err := w.scanOwned(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, "PORT_SCAN_FAILED")
	}
	out := &pb.ListPortsResponse{}
	for _, p := range ports { // Process names/PIDs are observations. No session is inferred from a port number.
		if r.GetSessionId() != "" && p.SessionId != r.SessionId {
			continue
		}
		out.Ports = append(out.Ports, proto.Clone(p).(*pb.OpenPort))
	}
	return out, nil
}
func (w *Worker) WatchPorts(parent context.Context, r *pb.WatchPortsRequest, send func(*pb.PortEvent) error) error {
	ctx, done, err := w.begin(parent, watchOperation)
	if err != nil {
		return err
	}
	defer done()
	previous := map[string]*pb.OpenPort{}
	ticker := time.NewTicker(w.config.ScanInterval)
	defer ticker.Stop()
	for {
		ports, err := w.scanOwned(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return connect.NewError(connect.CodeFailedPrecondition, "PORT_SCAN_FAILED")
		}
		next := map[string]*pb.OpenPort{}
		for _, p := range ports {
			if r.GetSessionId() != "" && p.SessionId != r.SessionId {
				continue
			}
			key := endpointKey(p)
			next[key] = p
		}
		var keys []string
		for k := range next {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := next[k]
			if old, ok := previous[k]; !ok || !proto.Equal(old, p) {
				if err = send(&pb.PortEvent{Type: pb.PortEvent_OPENED, Port: p}); err != nil {
					return err
				}
			}
		}
		keys = nil
		for k := range previous {
			if _, ok := next[k]; !ok {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err = send(&pb.PortEvent{Type: pb.PortEvent_CLOSED, Port: previous[k]}); err != nil {
				return err
			}
		}
		previous = next
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (w *Worker) ForwardPort(parent context.Context, s sdk.PortServerStream) error {
	ctx, done, err := w.begin(parent, connectionOperation)
	if err != nil {
		return err
	}
	defer done()
	first, err := receiveConnect(ctx, s, w.config.DialTimeout)
	if err != nil {
		return err
	}
	if err = tunnel.ConnectFrame(first); err != nil {
		return err
	}
	if _, err = w.checkPort(ctx, &pb.CheckPortRequest{RemoteAddress: first.RemoteAddress, Port: first.Port, SessionId: first.SessionId, Automatic: first.Automatic, SourceIdentity: first.SourceIdentity}); err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: w.config.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(first.RemoteAddress, strconv.Itoa(int(first.Port))))
	if err != nil {
		return connect.NewError(connect.CodeUnavailable, "PORT_TARGET_UNAVAILABLE")
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		conn.Close()
		return errors.New("non-TCP connection")
	}
	defer tcp.Close()
	if err = s.Send(&pb.PortData{Type: pb.PortData_CONNECTED, ConnectionId: first.ConnectionId}); err != nil {
		return err
	}
	return tunnel.Bridge(ctx, tcp, s, tunnel.Options{ID: first.ConnectionId, Idle: w.config.IdleTimeout, Server: true})
}

func receiveConnect(ctx context.Context, s sdk.PortServerStream, timeout time.Duration) (*pb.PortData, error) {
	type result struct {
		frame *pb.PortData
		err   error
	}
	received := make(chan result, 1)
	go func() { p, e := s.Receive(); received <- result{p, e} }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case v := <-received:
		return v.frame, v.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, connect.NewError(connect.CodeDeadlineExceeded, "CONNECT_TIMEOUT")
	}
}
