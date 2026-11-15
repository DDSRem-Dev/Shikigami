package plugin

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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
	if a.services.Region != nil {
		epb.RegisterRegionServiceServer(s, &regionServer{impl: a.services.Region})
	}
	if a.services.Browser != nil {
		epb.RegisterBrowserServiceServer(s, &browserServer{impl: a.services.Browser})
	}
	if a.services.BrowserLocal != nil {
		epb.RegisterBrowserLocalServiceServer(s, &browserLocalServer{impl: a.services.BrowserLocal})
	}
	return nil
}
func (a *adapter) GRPCClient(ctx context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	client := &lifecycleClient{client: ppb.NewPluginClient(c), done: ctx.Done()}
	client.files = &fileClient{owner: client, stub: ppb.NewFileServiceClient(c)}
	client.ports = &portClient{owner: client, stub: ppb.NewPortServiceClient(c)}
	client.sync = &syncClient{owner: client, stub: ppb.NewSyncWorkerClient(c)}
	client.port = &portWorkerClient{owner: client, stub: ppb.NewPortWorkerClient(c)}
	client.region = &regionClient{owner: client, stub: epb.NewRegionServiceClient(c)}
	client.browser = &browserClient{owner: client, stub: epb.NewBrowserServiceClient(c)}
	client.browserLocal = &browserLocalClient{owner: client, stub: epb.NewBrowserLocalServiceClient(c)}
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
	if err := validateServiceRegistration(side, impl.Info(), services); err != nil {
		return err
	}
	workerMethods := registeredServiceMethods(services)
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
				grpc.UnaryInterceptor(runtime.unaryAuthorization(workerMethods)),
				grpc.StreamInterceptor(runtime.streamAuthorization(workerMethods)))
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
	region          RegionClient
	browser         BrowserClient
	browserLocal    BrowserLocalClient
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
func (c *lifecycleClient) InvokeAction(ctx context.Context, req *ActionRequest) (*ActionResult, error) {
	in, err := MarshalActionRequest(req)
	if err != nil {
		return nil, err
	}
	out, err := c.client.InvokeAction(c.context(ctx), in)
	if err != nil {
		return nil, FromGRPC(err)
	}
	return UnmarshalActionResult(out)
}
func (c *lifecycleClient) Files() FileClient { return c.files }
func (c *lifecycleClient) Ports() PortClient { return c.ports }
func (c *lifecycleClient) Sync() SyncClient  { return c.sync }

func (c *lifecycleClient) Port() PortWorkerClient           { return c.port }
func (c *lifecycleClient) Region() RegionClient             { return c.region }
func (c *lifecycleClient) Browser() BrowserClient           { return c.browser }
func (c *lifecycleClient) BrowserLocal() BrowserLocalClient { return c.browserLocal }

var _ ExtensionLifecycle = (*lifecycleClient)(nil)
