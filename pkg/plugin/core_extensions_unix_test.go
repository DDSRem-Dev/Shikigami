//go:build darwin || linux

package plugin

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

type privateExtensionProbe struct {
	erpc.UnimplementedRegionServiceHandler
	erpc.UnimplementedBrowserServiceHandler
	streamExited  chan struct{}
	uploadEntered chan struct{}
	uploadExited  chan struct{}
}

func (p *privateExtensionProbe) InspectRegion(_ context.Context, req *epb.RegionInspectRequest) (*epb.RegionStatus, error) {
	return &epb.RegionStatus{Revision: req.ServerId}, nil
}
func (p *privateExtensionProbe) BrowserStream(ctx context.Context, stream erpc.BrowserServiceBrowserStreamServerStream) error {
	defer close(p.streamExited)
	first, err := stream.Receive()
	if err != nil {
		return err
	}
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_READY, ProfileId: first.ProfileId, InstanceId: "private-browser", AttachmentId: "private-attachment", ControllerId: "other-controller"}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}
func (p *privateExtensionProbe) WriteBrowserFile(_ context.Context, stream erpc.BrowserServiceWriteBrowserFileServerStream) (*epb.BrowserFile, error) {
	defer close(p.uploadExited)
	if _, err := stream.Receive(); err != nil {
		return nil, err
	}
	close(p.uploadEntered)
	_, err := stream.Receive()
	return nil, err
}

func TestPrivateCoreExtensionsCarryCredentialsAndCancel(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "shiki-core-ext-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "core.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		listener.Close()
		t.Fatal(err)
	}
	credentials := func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			info, ok := connect.CallInfoForServerContext(ctx)
			if !ok {
				return connect.NewError(connect.CodeUnauthenticated, "call metadata missing")
			}
			headers := info.RequestHeader()
			for key, value := range map[string]string{"Authorization": "Bearer instance-secret", "shiki-plugin-instance-id": "plugin-instance", "shiki-host-instance-id": "host-instance"} {
				if values := headers.Values(key); len(values) != 1 || values[0] != value {
					return connect.NewError(connect.CodeUnauthenticated, "instance credentials missing")
				}
			}
			return next(ctx, spec, stream)
		}
	}
	probe := &privateExtensionProbe{streamExited: make(chan struct{}), uploadEntered: make(chan struct{}), uploadExited: make(chan struct{})}
	server := connect.NewServer(credentials)
	erpc.RegisterRegionServiceHandler(server, probe)
	erpc.RegisterBrowserServiceHandler(server, probe)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server, connecthttp.WithCompressors())
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	httpServer := &http.Server{Handler: mux, Protocols: protocols}
	done := make(chan struct{})
	go func() { defer close(done); _ = httpServer.Serve(listener) }()
	t.Cleanup(func() { httpServer.Close(); <-done })
	core, err := newCoreClient(&ppb.InitRequest{Side: "client", PluginInstanceId: "plugin-instance", HostInstanceId: "host-instance", CoreApi: &ppb.CoreAPIEndpoint{Address: socket, Token: "instance-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	var legacy CoreAPI = core
	extensions, ok := legacy.(ExtensionCore)
	if !ok {
		t.Fatal("optional extension callback interface missing")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ctx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Authorization", "Bearer caller-must-not-override")
	status, err := extensions.Region().InspectRegion(ctx, &epb.RegionInspectRequest{ServerId: "explicit-server"})
	if err != nil || status.GetRevision() != "explicit-server" {
		t.Fatalf("private Region callback = %v, %v", status, err)
	}
	stream, err := extensions.Browser().BrowserStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err := stream.Send(&epb.BrowserFrame{Kind: epb.BrowserFrame_OPEN, ServerId: "explicit-server", ProfileId: "profile"}); err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Receive()
	if err != nil || ready.GetKind() != epb.BrowserFrame_READY || ready.GetInstanceId() != "private-browser" || ready.GetAttachmentId() != "private-attachment" || ready.GetControllerId() != "other-controller" {
		t.Fatalf("private Browser callback = %v, %v", ready, err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.streamExited:
	case <-ctx.Done():
		t.Fatal("private browser attachment survived SDK Close")
	}
	upload, err := extensions.Browser().WriteBrowserFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer upload.Close()
	if err := upload.Send(&epb.BrowserFileChunk{ServerId: "explicit-server", ProfileId: "profile", Name: "upload.txt", TotalSize: 1, RequestId: "request"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.uploadEntered:
	case <-ctx.Done():
		t.Fatal("private browser upload did not reach handler")
	}
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.uploadExited:
	case <-ctx.Done():
		t.Fatal("private browser upload survived SDK Close")
	}
}
