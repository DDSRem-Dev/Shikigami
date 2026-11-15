package plugin

import (
	"context"
	"net"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type extensionRegistrationProbe struct {
	RegionService
	BrowserService
	BrowserLocalService
}

func TestExtensionCapabilitiesMatchRegisteredSide(t *testing.T) {
	probe := &extensionRegistrationProbe{}
	for _, test := range []struct {
		id, side, capability string
		services             Services
	}{
		{"region", "server", RegionCapability, Services{Region: probe}},
		{"browser", "server", BrowserCapability, Services{Browser: probe}},
		{"browser", "client", BrowserViewerCapability, Services{BrowserLocal: probe}},
	} {
		t.Run(test.id+"/"+test.side, func(t *testing.T) {
			offer, err := BuiltinProtocol(test.id, test.side)
			if err != nil {
				t.Fatal(err)
			}
			if len(offer.Capabilities) != 2 || offer.Capabilities[0] != CoreCapability || offer.Capabilities[1] != test.capability {
				t.Fatalf("unexpected capabilities: %v", offer.Capabilities)
			}
			info := &ppb.PluginInfo{Protocol: offer}
			if err := validateServiceRegistration(test.side, info, test.services); err != nil {
				t.Fatal(err)
			}
			if err := validateServiceRegistration(test.side, info, Services{}); err == nil {
				t.Fatal("advertised capability accepted without service")
			}
			if err := validateServiceRegistration(test.side, &ppb.PluginInfo{Protocol: &pb.ProtocolOffer{}}, test.services); err == nil {
				t.Fatal("service accepted without advertised capability")
			}
			other := "client"
			if test.side == "client" {
				other = "server"
			}
			if err := validateServiceRegistration(other, info, test.services); err == nil {
				t.Fatal("service accepted on wrong side")
			}
		})
	}
	if _, err := BuiltinProtocol("region", "client"); err == nil {
		t.Fatal("region offered a client worker")
	}
}

func TestExtensionMethodAllowlistMatchesOnlyRegisteredServices(t *testing.T) {
	probe := &extensionRegistrationProbe{}
	for _, test := range []struct {
		name     string
		services Services
	}{
		{"RegionService", Services{Region: probe}},
		{"BrowserService", Services{Browser: probe}},
		{"BrowserLocalService", Services{BrowserLocal: probe}},
	} {
		methods := registeredServiceMethods(test.services)
		descriptors := epb.File_extensions_proto.Services()
		for i := range descriptors.Len() {
			service := descriptors.Get(i)
			for j := range service.Methods().Len() {
				procedure := "/" + string(service.FullName()) + "/" + string(service.Methods().Get(j).Name())
				if got, want := methods[procedure], string(service.Name()) == test.name; got != want {
					t.Fatalf("%s registration: method %s permitted=%v; want %v", test.name, procedure, got, want)
				}
			}
		}
	}
}

type extensionContextStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s extensionContextStream) Context() context.Context { return s.ctx }

func TestExtensionMethodsRequireExactInstanceCredentials(t *testing.T) {
	probe := &extensionRegistrationProbe{}
	services := Services{Region: probe, Browser: probe, BrowserLocal: probe}
	methods := registeredServiceMethods(services)
	r := &runtime{instance: "instance", token: "secret"}
	unary := r.unaryAuthorization(methods)
	stream := r.streamAuthorization(methods)
	for _, md := range []metadata.MD{
		nil,
		metadata.Pairs("shiki-plugin-instance-id", "other", "authorization", "Bearer secret"),
		metadata.Pairs("shiki-plugin-instance-id", "instance", "authorization", "Bearer wrong"),
		metadata.Pairs("shiki-plugin-instance-id", "instance", "authorization", "Bearer secret", "authorization", "Bearer secret"),
		metadata.Pairs("shiki-plugin-instance-id", "instance", "shiki-plugin-instance-id", "instance", "authorization", "Bearer secret"),
	} {
		ctx := metadata.NewIncomingContext(t.Context(), md)
		_, err := unary(ctx, nil, &grpc.UnaryServerInfo{FullMethod: epb.RegionService_ApplyRegion_FullMethodName}, func(context.Context, any) (any, error) {
			t.Fatal("unauthenticated unary reached implementation")
			return nil, nil
		})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatal(err)
		}
		err = stream(nil, extensionContextStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: epb.BrowserService_BrowserStream_FullMethodName}, func(any, grpc.ServerStream) error {
			t.Fatal("unauthenticated stream reached implementation")
			return nil
		})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatal(err)
		}
	}
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("shiki-plugin-instance-id", "instance", "authorization", "Bearer secret"))
	for _, method := range []string{epb.RegionService_InspectRegion_FullMethodName, epb.BrowserLocalService_OpenBrowserViewer_FullMethodName} {
		called := false
		_, err := unary(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) { called = true; return nil, nil })
		if err != nil || !called {
			t.Fatalf("authenticated method %s failed: %v", method, err)
		}
	}
	for _, method := range []string{"/shiki.extensions.v1.BrowserService/FutureMethod", epb.RegionService_InspectRegion_FullMethodName} {
		_, err := r.unaryAuthorization(registeredServiceMethods(Services{}))(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) {
			t.Fatal("unregistered method reached implementation")
			return nil, nil
		})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatal(err)
		}
	}
}

func nativeExtensionClient(t *testing.T, services Services, options ...grpc.ServerOption) (*lifecycleClient, *grpc.ClientConn) {
	t.Helper()
	r := &runtime{instance: "instance", token: "secret", state: "running"}
	methods := registeredServiceMethods(services)
	options = append([]grpc.ServerOption{grpc.UnaryInterceptor(r.unaryAuthorization(methods)), grpc.StreamInterceptor(r.streamAuthorization(methods))}, options...)
	server := grpc.NewServer(options...)
	adapter := &adapter{runtime: r, services: services}
	if err := adapter.GRPCServer(nil, server); err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(64 << 10)
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); listener.Close(); <-done })
	conn, err := grpc.NewClient("passthrough:///sdk-extensions", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	value, err := adapter.GRPCClient(t.Context(), nil, conn)
	if err != nil {
		t.Fatal(err)
	}
	client := value.(*lifecycleClient)
	client.instance, client.token = "instance", "secret"
	return client, conn
}

type browserStreamProbe struct {
	BrowserService
	opened chan *epb.BrowserFrame
	exited chan struct{}
}

func (p *browserStreamProbe) BrowserStream(ctx context.Context, stream BrowserServerStream) error {
	defer close(p.exited)
	frame, err := stream.Receive()
	if err != nil {
		return err
	}
	p.opened <- frame
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_READY, InstanceId: "browser-instance", AttachmentId: "attachment", ControllerId: "other-controller"}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestNativeBrowserStreamPreservesGrantsAndCloseCancels(t *testing.T) {
	probe := &browserStreamProbe{opened: make(chan *epb.BrowserFrame, 1), exited: make(chan struct{})}
	client, _ := nativeExtensionClient(t, Services{Browser: probe})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	stream, err := client.Browser().BrowserStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_OPEN, ServerId: "server", ProfileId: "profile", OwnerId: "authenticated-owner", CanControl: true, ReadOnly: false}); err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Receive()
	if err != nil || ready.GetKind() != epb.BrowserFrame_READY || ready.GetAttachmentId() != "attachment" || ready.GetControllerId() != "other-controller" {
		t.Fatalf("READY = %v, %v", ready, err)
	}
	first := <-probe.opened
	if first.OwnerId != "authenticated-owner" || !first.CanControl || first.ReadOnly || first.ServerId != "server" || first.ProfileId != "profile" {
		t.Fatalf("host-owned grants changed: %v", first)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.exited:
	case <-ctx.Done():
		t.Fatal("Close did not cancel native browser attachment")
	}
}

func TestBrowserDownloadKeepsLocalSizeErrorAndCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	download := &browserDownload{receive: func() (*epb.BrowserFileChunk, error) {
		return &epb.BrowserFileChunk{Data: make([]byte, limits.MaxDataBytes+1)}, nil
	}, cancel: cancel}
	if _, err := download.Receive(); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("local validation lost its code: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("invalid download did not cancel transport")
	}
}

type regionInspectProbe struct {
	erpc.RegionServiceClient
	calls int
}

func (p *regionInspectProbe) InspectRegion(context.Context, *epb.RegionInspectRequest) (*epb.RegionStatus, error) {
	p.calls++
	return &epb.RegionStatus{}, nil
}

func TestCoreExtensionScopesAreExplicitExceptServerLocalInspect(t *testing.T) {
	probe := &regionInspectProbe{}
	client := &coreRegionClient{stub: probe}
	if _, err := client.InspectRegion(t.Context(), &epb.RegionInspectRequest{}); connect.CodeOf(err) != connect.CodeInvalidArgument || probe.calls != 0 {
		t.Fatal("client implicit server scope accepted", err)
	}
	client.localInspect = true
	if _, err := client.InspectRegion(t.Context(), &epb.RegionInspectRequest{}); err != nil || probe.calls != 1 {
		t.Fatal("server current-host inspect failed", err)
	}
	if _, err := client.ApplyRegion(t.Context(), &epb.RegionApplyRequest{}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("server-local exception extended to mutation", err)
	}
	browser := &coreBrowserClient{}
	if _, err := browser.GetBrowserStatus(t.Context(), &epb.BrowserScope{}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("implicit browser scope accepted", err)
	}
	stream := &coreBrowserStream{opened: true, server: "server", profile: "profile"}
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_DATA, ServerId: "other"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("browser stream route changed", err)
	}
	writer := &coreBrowserWriter{opened: true, server: "server", profile: "profile"}
	if err := writer.Send(&epb.BrowserFileChunk{ProfileId: "other"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("browser upload profile changed", err)
	}
}
