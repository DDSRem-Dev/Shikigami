package plugin

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const DispenseName = "shiki"

var Handshake = goplugin.HandshakeConfig{ProtocolVersion: 1, MagicCookieKey: "SHIKI_PLUGIN", MagicCookieValue: "shikigami-plugin-v1"}

type adapter struct {
	goplugin.NetRPCUnsupportedPlugin
	runtime  *runtime
	services Services
}

func (a *adapter) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	ppb.RegisterPluginServer(s, a.runtime)
	if a.services.Files != nil {
		ppb.RegisterFileServiceServer(s, &fileServer{impl: a.services.Files})
	}
	if a.services.Ports != nil {
		ppb.RegisterPortServiceServer(s, &portServer{impl: a.services.Ports})
	}
	if a.services.Sync != nil {
		ppb.RegisterSyncWorkerServer(s, &syncServer{impl: a.services.Sync})
	}
	if a.services.Port != nil {
		ppb.RegisterPortWorkerServer(s, &portWorkerServer{impl: a.services.Port})
	}
	return nil
}
func (a *adapter) GRPCClient(ctx context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	client := &lifecycleClient{client: ppb.NewPluginClient(c), done: ctx.Done()}
	client.files = &fileClient{owner: client, stub: ppb.NewFileServiceClient(c)}
	client.ports = &portClient{owner: client, stub: ppb.NewPortServiceClient(c)}
	client.sync = &syncClient{owner: client, stub: ppb.NewSyncWorkerClient(c)}
	client.port = &portWorkerClient{owner: client, stub: ppb.NewPortWorkerClient(c)}
	return client, nil
}

// ClientPlugin is an opaque go-plugin factory. The dispensed value implements
// Lifecycle; raw connections remain inside the SDK adapter.
func ClientPlugin() goplugin.Plugin { return &adapter{} }

// Serve blocks as the plugin process entry point. go-plugin supplies AutoMTLS
// credentials and supervises its parent connection. Only native plugin services
// are registered here; CoreAPI is exclusively a Connect callback on the host.
func Serve(side string, impl Implementation, available ...Services) error {
	if side != "client" && side != "server" {
		return errors.New("invalid plugin side")
	}
	if len(available) > 1 {
		return errors.New("only one service set may be supplied")
	}
	var services Services
	if len(available) == 1 {
		services = available[0]
	}
	if side != "server" && (services.Files != nil || services.Ports != nil) {
		return errors.New("server data services cannot run on a client plugin")
	}
	if side != "client" && (services.Sync != nil || services.Port != nil) {
		return errors.New("local worker requires client side")
	}
	info := impl.Info()
	if info == nil || info.Protocol == nil {
		return errors.New("plugin protocol offer is required")
	}
	capabilities := map[string]bool{}
	for _, capability := range info.Protocol.Capabilities {
		capabilities[capability] = true
	}
	if side == "server" && ((services.Files != nil) != capabilities[FileCapability] || (services.Ports != nil) != capabilities[PortCapability]) {
		return errors.New("server worker capabilities must match registered services")
	}
	if side == "client" && (services.Sync != nil) != capabilities[SyncCapability] {
		return errors.New("client sync capability must match registered worker")
	}
	if side == "client" && (services.Port != nil) != capabilities[PortWorkerCapability] {
		return errors.New("client port capability must match registered worker")
	}
	workerMethods := map[string]bool{}
	if services.Port != nil {
		for _, method := range []string{ppb.PortWorker_DiscoverForwards_FullMethodName, ppb.PortWorker_StartForward_FullMethodName, ppb.PortWorker_StopForward_FullMethodName, ppb.PortWorker_ListForwards_FullMethodName} {
			workerMethods[method] = true
		}
	}
	if services.Files != nil {
		for _, method := range []string{ppb.FileService_ListFiles_FullMethodName, ppb.FileService_StatFile_FullMethodName, ppb.FileService_ReadFile_FullMethodName, ppb.FileService_WriteFile_FullMethodName, ppb.FileService_DeleteFile_FullMethodName, ppb.FileService_WatchFiles_FullMethodName} {
			workerMethods[method] = true
		}
	}
	if services.Ports != nil {
		for _, method := range []string{ppb.PortService_CheckPort_FullMethodName, ppb.PortService_ListPorts_FullMethodName, ppb.PortService_WatchPorts_FullMethodName, ppb.PortService_ForwardPort_FullMethodName} {
			workerMethods[method] = true
		}
	}
	if services.Sync != nil {
		for _, method := range []string{ppb.SyncWorker_ConfigureMapping_FullMethodName, ppb.SyncWorker_GetSyncState_FullMethodName, ppb.SyncWorker_ListConflicts_FullMethodName, ppb.SyncWorker_ResolveConflict_FullMethodName, ppb.SyncWorker_ReadConflictFile_FullMethodName} {
			workerMethods[method] = true
		}
	}
	runtime := &runtime{impl: impl, side: side}
	parent := os.Getppid()
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-ticker.C:
				if os.Getppid() == parent {
					continue
				}
				// Parent death must end workers even if go-plugin's control
				// channel disappears without a graceful Shutdown RPC.
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				stopped := make(chan struct{})
				go func() { _, _ = runtime.Stop(ctx, &pb.Empty{}); close(stopped) }()
				select {
				case <-stopped:
				case <-ctx.Done():
				}
				cancel()
				os.Exit(0)
			}
		}
	}()
	goplugin.Serve(&goplugin.ServeConfig{HandshakeConfig: Handshake, Plugins: goplugin.PluginSet{DispenseName: &adapter{runtime: runtime, services: services}}, Logger: hclog.NewNullLogger(),
		GRPCServer: func(options []grpc.ServerOption) *grpc.Server {
			options = append(options, grpc.MaxRecvMsgSize(limits.MaxMessageBytes), grpc.MaxSendMsgSize(limits.MaxMessageBytes),
				grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
					switch info.FullMethod {
					case "/grpc.health.v1.Health/Check", "/plugin.GRPCController/Shutdown", ppb.Plugin_GetInfo_FullMethodName, ppb.Plugin_Init_FullMethodName:
					case ppb.Plugin_Start_FullMethodName, ppb.Plugin_Stop_FullMethodName, ppb.Plugin_Health_FullMethodName:
						if !runtime.authorized(ctx) {
							return nil, status.Error(codes.Unauthenticated, "plugin instance authentication required")
						}
					default:
						if !workerMethods[info.FullMethod] {
							return nil, status.Error(codes.PermissionDenied, "plugin method not permitted")
						}
						if !runtime.authorized(ctx) {
							return nil, status.Error(codes.Unauthenticated, "plugin instance authentication required")
						}
					}
					return next(ctx, req)
				}),
				grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
					switch info.FullMethod {
					case "/plugin.GRPCBroker/StartStream", "/plugin.GRPCStdio/StreamStdio":
						return next(srv, stream)
					default:
						if !workerMethods[info.FullMethod] {
							return status.Error(codes.PermissionDenied, "plugin stream not permitted")
						}
						if !runtime.authorized(stream.Context()) {
							return status.Error(codes.Unauthenticated, "plugin instance authentication required")
						}
						return next(srv, stream)
					}
				}))
			return grpc.NewServer(options...)
		},
	})
	return nil
}

type lifecycleClient struct {
	client          ppb.PluginClient
	done            <-chan struct{}
	instance, token string
	files           FileClient
	ports           PortClient
	sync            SyncClient
	port            PortWorkerClient
}

func (c *lifecycleClient) context(ctx context.Context) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("shiki-plugin-instance-id", c.instance, "authorization", "Bearer "+c.token))
}
func (c *lifecycleClient) GetInfo(ctx context.Context) (*ppb.PluginInfo, error) {
	result, err := c.client.GetInfo(ctx, &pb.Empty{})
	return result, FromGRPC(err)
}
func (c *lifecycleClient) Init(ctx context.Context, req *ppb.InitRequest) error {
	_, err := c.client.Init(ctx, req)
	if err == nil {
		c.instance = req.PluginInstanceId
		c.token = req.GetCoreApi().GetToken()
	}
	return FromGRPC(err)
}
func (c *lifecycleClient) Start(ctx context.Context) error {
	_, err := c.client.Start(c.context(ctx), &pb.Empty{})
	return FromGRPC(err)
}
func (c *lifecycleClient) Stop(ctx context.Context) error {
	_, err := c.client.Stop(c.context(ctx), &pb.Empty{})
	return FromGRPC(err)
}
func (c *lifecycleClient) Health(ctx context.Context) (*pb.HealthStatus, error) {
	result, err := c.client.Health(c.context(ctx), &pb.Empty{})
	return result, FromGRPC(err)
}
func (c *lifecycleClient) Done() <-chan struct{} { return c.done }
func (c *lifecycleClient) Files() FileClient     { return c.files }
func (c *lifecycleClient) Ports() PortClient     { return c.ports }
func (c *lifecycleClient) Sync() SyncClient      { return c.sync }

func (c *lifecycleClient) Port() PortWorkerClient { return c.port }
