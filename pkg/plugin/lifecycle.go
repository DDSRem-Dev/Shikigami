package plugin

import (
	"context"
	"crypto/subtle"
	"errors"
	"sync"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// InitContext contains only serialized configuration plus an SDK callback.
// No host process pointer or remote user token is passed to a plugin.
type InitContext struct {
	Config                                     *structpb.Struct
	Core                                       CoreAPI
	Side, InstanceID, HostInstanceID, StateDir string
	Protocol                                   *pb.ProtocolAgreement
}

type Implementation interface {
	Info() *ppb.PluginInfo
	Init(context.Context, InitContext) error
	// Start launches work with the supplied lifetime context and returns when
	// ready. This context survives the Start RPC and ends on Stop/process exit.
	Start(context.Context) error
	Stop(context.Context) error
	Health(context.Context) (*pb.HealthStatus, error)
}

// Lifecycle is the host's typed view of the go-plugin subprocess.
type Lifecycle interface {
	GetInfo(context.Context) (*ppb.PluginInfo, error)
	Init(context.Context, *ppb.InitRequest) error
	Start(context.Context) error
	Stop(context.Context) error
	Health(context.Context) (*pb.HealthStatus, error)
	Done() <-chan struct{}
	Files() FileClient
	Ports() PortClient
	Sync() SyncClient
	Port() PortWorkerClient
}

type runtime struct {
	ppb.UnimplementedPluginServer
	impl            Implementation
	side            string
	operations      sync.Mutex
	mu              sync.Mutex
	instance, token string
	state           string
	core            *coreClient
	cancel          context.CancelFunc
}

func (r *runtime) GetInfo(context.Context, *pb.Empty) (*ppb.PluginInfo, error) {
	info := r.impl.Info()
	if info == nil {
		return nil, ToGRPC(errors.New("plugin info missing"))
	}
	return proto.Clone(info).(*ppb.PluginInfo), nil
}
func (r *runtime) Init(ctx context.Context, req *ppb.InitRequest) (*pb.Empty, error) {
	r.operations.Lock()
	defer r.operations.Unlock()
	r.mu.Lock()
	already := r.state != ""
	r.mu.Unlock()
	if already {
		return nil, ToGRPC(connect.NewError(connect.CodeFailedPrecondition, "already initialized"))
	}
	info := r.impl.Info()
	if info == nil || req.Side != r.side || (info.Side != "both" && info.Side != r.side) || req.Protocol == nil {
		return nil, ToGRPC(connect.NewError(connect.CodeFailedPrecondition, "plugin side or protocol mismatch"))
	}
	offered := &pb.ProtocolOffer{Major: req.Protocol.Major, MinMinor: req.Protocol.Minor, MaxMinor: req.Protocol.Minor, Capabilities: req.Protocol.Capabilities, RequiredCapabilities: req.Protocol.Capabilities}
	if _, err := version.Negotiate(info.Protocol, offered, info.Version); err != nil {
		return nil, ToGRPC(err)
	}
	core, err := newCoreClient(req)
	if err != nil {
		return nil, ToGRPC(err)
	}
	if err = r.impl.Init(ctx, InitContext{Config: req.Config, Core: core, Side: req.Side, InstanceID: req.PluginInstanceId, HostInstanceID: req.HostInstanceId, StateDir: req.StateDir, Protocol: req.Protocol}); err != nil {
		core.Close()
		return nil, ToGRPC(err)
	}
	r.mu.Lock()
	r.core = core
	r.instance = req.PluginInstanceId
	r.token = req.CoreApi.Token
	r.state = "initialized"
	r.mu.Unlock()
	return &pb.Empty{}, nil
}
func (r *runtime) Start(ctx context.Context, _ *pb.Empty) (*pb.Empty, error) {
	r.operations.Lock()
	defer r.operations.Unlock()
	r.mu.Lock()
	state := r.state
	r.mu.Unlock()
	if state == "running" {
		return &pb.Empty{}, nil
	}
	if state != "initialized" {
		return nil, ToGRPC(connect.NewError(connect.CodeFailedPrecondition, "plugin not initialized"))
	}
	// Intentionally independent of the short Start RPC; Stop owns cancellation.
	lifetime, cancel := context.WithCancel(context.Background())
	if err := r.impl.Start(lifetime); err != nil {
		cancel()
		return nil, ToGRPC(err)
	}
	r.mu.Lock()
	r.cancel = cancel
	r.state = "running"
	r.mu.Unlock()
	return &pb.Empty{}, nil
}
func (r *runtime) Stop(ctx context.Context, _ *pb.Empty) (*pb.Empty, error) {
	r.operations.Lock()
	defer r.operations.Unlock()
	r.mu.Lock()
	state := r.state
	cancel := r.cancel
	core := r.core
	r.mu.Unlock()
	if state == "stopped" || state == "" {
		return &pb.Empty{}, nil
	}
	if cancel != nil {
		cancel()
	}
	err := r.impl.Stop(ctx)
	if core != nil {
		core.Close()
	}
	r.mu.Lock()
	if err == nil {
		r.state = "stopped"
	} else {
		// Cancellation requests shutdown; only the implementation can confirm
		// it has drained. Preserve Stop retries after a caller's deadline.
		r.state = "stopping"
	}
	r.mu.Unlock()
	if err != nil {
		return nil, ToGRPC(err)
	}
	return &pb.Empty{}, nil
}
func (r *runtime) Health(ctx context.Context, _ *pb.Empty) (*pb.HealthStatus, error) {
	health, err := r.impl.Health(ctx)
	return health, ToGRPC(err)
}
func (r *runtime) authorized(ctx context.Context) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}
	ids, tokens := md.Get("shiki-plugin-instance-id"), md.Get("authorization")
	if len(ids) != 1 || len(tokens) != 1 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.instance != "" && ids[0] == r.instance && subtle.ConstantTimeCompare([]byte(tokens[0]), []byte("Bearer "+r.token)) == 1
}
