package plugin_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

var seed = []byte{0, 0xff, 27, '[', '3', '1', 'm', 'x'}

func hash(data []byte) string { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }
func scenarioError(path string) error {
	for _, code := range []connect.Code{connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeAborted, connect.CodeResourceExhausted} {
		if path == "error/"+code.String() {
			return connect.NewError(code, "secret worker content and credentials")
		}
	}
	return nil
}

type memoryWorker struct{ plugin *helperPlugin }

func (w *memoryWorker) ListFiles(context.Context, *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	return &pb.ListFilesResponse{Files: []*pb.FileInfo{{Path: "seed", Size: int64(len(seed)), ContentHash: hash(seed)}}}, nil
}
func (w *memoryWorker) StatFile(ctx context.Context, r *pb.StatFileRequest) (*pb.FileInfo, error) {
	if e := scenarioError(r.Path); e != nil {
		return nil, e
	}
	return &pb.FileInfo{Path: r.Path, Size: int64(len(seed)), ContentHash: hash(seed)}, nil
}
func (w *memoryWorker) DeleteFile(context.Context, *pb.DeleteFileRequest) error { return nil }
func (w *memoryWorker) ReadFile(ctx context.Context, r *pb.ReadFileRequest, send func(*pb.FileData) error) error {
	if err := send(&pb.FileData{Payload: &pb.FileData_Chunk{Chunk: seed}}); err != nil {
		return err
	}
	return scenarioError(r.Path)
}
func (w *memoryWorker) WriteFile(ctx context.Context, receive func(context.Context) (*pb.WriteFileRequest, error)) (*pb.FileInfo, error) {
	first, e := receive(ctx)
	if e != nil {
		return nil, e
	}
	header := first.GetHeader()
	w.notify("worker_upload_open")
	var content []byte
	for {
		message, e := receive(ctx)
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			w.notifyCanceled()
			return nil, e
		}
		content = append(content, message.GetChunk()...)
	}
	if header.Path == "wait" {
		<-ctx.Done()
		w.notifyCanceled()
		return nil, ctx.Err()
	}
	if e := scenarioError(header.Path); e != nil {
		return nil, e
	}
	return &pb.FileInfo{Path: header.Path, Size: int64(len(content)), ContentHash: hash(content)}, nil
}
func (w *memoryWorker) WatchFiles(ctx context.Context, r *pb.WatchFilesRequest, send func(*pb.FileEvent) error) error {
	<-ctx.Done()
	return ctx.Err()
}
func (w *memoryWorker) ListPorts(context.Context, *pb.ListPortsRequest) (*pb.ListPortsResponse, error) {
	return &pb.ListPortsResponse{Ports: []*pb.OpenPort{{Port: 8080, Protocol: "tcp", BindAddress: "127.0.0.1"}}}, nil
}
func (w *memoryWorker) WatchPorts(ctx context.Context, r *pb.WatchPortsRequest, send func(*pb.PortEvent) error) error {
	<-ctx.Done()
	return ctx.Err()
}
func (w *memoryWorker) ForwardPort(ctx context.Context, s sdk.PortServerStream) error {
	open, e := s.Receive()
	if e != nil {
		return e
	}
	if e = s.Send(&pb.PortData{Type: pb.PortData_CONNECTED, ConnectionId: open.ConnectionId}); e != nil {
		return e
	}
	if open.Port == 8082 {
		<-ctx.Done()
		w.notifyCanceled()
		return ctx.Err()
	}
	if open.Port == 8081 {
		return connect.NewError(connect.CodeAborted, "secret port failure")
	}
	var data []byte
	halfClosed := false
	for {
		frame, e := s.Receive()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return e
		}
		switch frame.Type {
		case pb.PortData_DATA:
			data = append(data, frame.Data...)
		case pb.PortData_HALF_CLOSE:
			halfClosed = true
		default:
			return connect.NewError(connect.CodeInvalidArgument, "unexpected port frame")
		}
	}
	if !halfClosed {
		return connect.NewError(connect.CodeFailedPrecondition, "TCP half-close frame missing")
	}
	if e = s.Send(&pb.PortData{Type: pb.PortData_DATA, Data: append(data, []byte(" after EOF")...)}); e != nil {
		return e
	}
	return s.Send(&pb.PortData{Type: pb.PortData_HALF_CLOSE})
}
func (w *memoryWorker) notifyCanceled() {
	w.notify("worker_canceled")
}
func (w *memoryWorker) notify(event string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = w.plugin.core.PublishEvent(ctx, &ppb.PublishEventRequest{ExpectedHostInstanceId: w.plugin.host, Event: &ppb.Event{Type: event, Scope: grantedScope}})
}

func (p *helperPlugin) chainHealth(ctx context.Context) (*pb.HealthStatus, error) {
	if p.mode == "chain-unary-error" {
		_, e := p.core.RemoteStatFile(ctx, &ppb.RemoteStatFileRequest{Scope: grantedScope, Request: &pb.StatFileRequest{ProjectId: "proj-test", Path: "error/aborted"}})
		return nil, e
	}
	if p.mode == "chain-upload-error" || p.mode == "chain-upload-limit" || p.mode == "chain-upload-wait" {
		write, e := p.core.RemoteWriteFile(ctx)
		if e != nil {
			return nil, e
		}
		path := "upload"
		if p.mode == "chain-upload-wait" {
			path = "wait"
		}
		if p.mode == "chain-upload-error" {
			path = "error/aborted"
		}
		if e = write.Send(&ppb.RemoteWriteFrame{Payload: &ppb.RemoteWriteFrame_Open{Open: &ppb.RemoteWriteOpen{Scope: grantedScope, Header: &pb.WriteFileHeader{ProjectId: "proj-test", Path: path}}}}); e != nil {
			return nil, e
		}
		chunk := seed
		if p.mode == "chain-upload-limit" {
			chunk = make([]byte, connection.MaxDataBytes+1)
		}
		if e = write.Send(&ppb.RemoteWriteFrame{Payload: &ppb.RemoteWriteFrame_Chunk{Chunk: chunk}}); e != nil {
			return nil, e
		}
		if p.mode == "chain-upload-wait" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		_, e = write.CloseAndReceive()
		return nil, e
	}
	if p.mode == "chain-port-error" {
		port, e := p.core.RemoteForwardPort(ctx)
		if e != nil {
			return nil, e
		}
		defer port.Close()
		if e = port.Send(&ppb.RemotePortFrame{Payload: &ppb.RemotePortFrame_Open{Open: &ppb.RemotePortOpen{Scope: grantedScope, Connect: &pb.PortData{Type: pb.PortData_CONNECT, Port: 8081}}}}); e != nil {
			return nil, e
		}
		if _, e = port.Receive(); e != nil {
			return nil, e
		}
		_, e = port.Receive()
		return nil, e
	}
	if strings.HasPrefix(p.mode, "chain-error-") {
		path := "error/" + strings.TrimPrefix(p.mode, "chain-error-")
		stream, e := p.core.RemoteReadFile(ctx, &ppb.RemoteReadFileRequest{Scope: grantedScope, Request: &pb.ReadFileRequest{ProjectId: "proj-test", Path: path}})
		if e != nil {
			return nil, e
		}
		defer stream.Close()
		if _, e = stream.Receive(); e != nil {
			return nil, e
		}
		_, e = stream.Receive()
		return nil, e
	}
	if p.mode == "chain-cancel" {
		call, cancel := context.WithCancel(ctx)
		defer cancel()
		stream, e := p.core.RemoteForwardPort(call)
		if e != nil {
			return nil, e
		}
		defer stream.Close()
		if e = stream.Send(&ppb.RemotePortFrame{Payload: &ppb.RemotePortFrame_Open{Open: &ppb.RemotePortOpen{Scope: grantedScope, Connect: &pb.PortData{Type: pb.PortData_CONNECT, Port: 8082, RemoteAddress: "127.0.0.1"}}}}); e != nil {
			return nil, e
		}
		if _, e = stream.Receive(); e != nil {
			return nil, e
		}
		cancel()
		if _, e = stream.Receive(); connect.CodeOf(e) != connect.CodeCanceled {
			return nil, fmt.Errorf("cancellation failed: %v", e)
		}
		return &pb.HealthStatus{Status: pb.HealthStatus_HEALTHY}, nil
	}
	info, e := p.core.RemoteStatFile(ctx, &ppb.RemoteStatFileRequest{Scope: grantedScope, Request: &pb.StatFileRequest{ProjectId: "proj-test", Path: "seed"}})
	if e != nil {
		return nil, e
	}
	if info.ContentHash != hash(seed) {
		return nil, errors.New("unary hash mismatch")
	}
	read, e := p.core.RemoteReadFile(ctx, &ppb.RemoteReadFileRequest{Scope: grantedScope, Request: &pb.ReadFileRequest{ProjectId: "proj-test", Path: "seed"}})
	if e != nil {
		return nil, e
	}
	defer read.Close()
	var downloaded []byte
	for {
		data, e := read.Receive()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
		downloaded = append(downloaded, data.GetChunk()...)
	}
	if !bytes.Equal(downloaded, seed) {
		return nil, errors.New("download bytes changed")
	}
	write, e := p.core.RemoteWriteFile(ctx)
	if e != nil {
		return nil, e
	}
	if e = write.Send(&ppb.RemoteWriteFrame{Payload: &ppb.RemoteWriteFrame_Open{Open: &ppb.RemoteWriteOpen{Scope: grantedScope, Header: &pb.WriteFileHeader{ProjectId: "proj-test", Path: "upload"}}}}); e != nil {
		return nil, e
	}
	for range 3 {
		if e = write.Send(&ppb.RemoteWriteFrame{Payload: &ppb.RemoteWriteFrame_Chunk{Chunk: seed}}); e != nil {
			return nil, e
		}
	}
	committed, e := write.CloseAndReceive()
	if e != nil {
		return nil, e
	}
	if committed.Size != int64(3*len(seed)) || committed.ContentHash != hash(bytes.Repeat(seed, 3)) {
		return nil, errors.New("upload final acknowledgment differs")
	}
	port, e := p.core.RemoteForwardPort(ctx)
	if e != nil {
		return nil, e
	}
	defer port.Close()
	if e = port.Send(&ppb.RemotePortFrame{Payload: &ppb.RemotePortFrame_Open{Open: &ppb.RemotePortOpen{Scope: grantedScope, Connect: &pb.PortData{Type: pb.PortData_CONNECT, Port: 8080, RemoteAddress: "127.0.0.1"}}}}); e != nil {
		return nil, e
	}
	ready, e := port.Receive()
	if e != nil {
		return nil, e
	}
	if ready.Type != pb.PortData_CONNECTED {
		return nil, errors.New("port CONNECTED missing before CloseSend")
	}
	for _, frame := range []*pb.PortData{{Type: pb.PortData_DATA, Data: seed}, {Type: pb.PortData_HALF_CLOSE}} {
		if e = port.Send(&ppb.RemotePortFrame{Payload: &ppb.RemotePortFrame_Data{Data: frame}}); e != nil {
			return nil, e
		}
	}
	if e = port.CloseSend(); e != nil {
		return nil, e
	}
	response, e := port.Receive()
	if e != nil {
		return nil, e
	}
	if !bytes.Equal(response.Data, append(append([]byte(nil), seed...), []byte(" after EOF")...)) {
		return nil, errors.New("half-close truncated remaining response")
	}
	last, e := port.Receive()
	if e != nil || last.GetType() != pb.PortData_HALF_CLOSE {
		return nil, errors.New("response HALF_CLOSE missing")
	}
	if _, e = port.Receive(); !errors.Is(e, io.EOF) {
		return nil, e
	}
	return &pb.HealthStatus{Status: pb.HealthStatus_HEALTHY, Details: map[string]string{"shapes": "unary,server,client,bidi", "final_status": "verified"}}, nil
}

type chainCore struct {
	*host.RemoteCore
	probe *coreProbe
}

func (c *chainCore) GetConfig(ctx context.Context, r *ppb.ConfigRequest) (*ppb.PluginConfig, error) {
	return c.probe.GetConfig(ctx, r)
}
func (c *chainCore) SubscribeEvents(ctx context.Context, r *ppb.SubscribeRequest, s prpc.CoreAPISubscribeEventsServerStream) error {
	return c.probe.SubscribeEvents(ctx, r, s)
}

func setupChain(t *testing.T, mode string) (*host.Instance, *coreProbe) {
	t.Helper()
	serverConfig, serverProbe := pluginConfig(t, "")
	serverConfig.Manifest.Side = "server"
	serverConfig.Policy.Side = "server"
	serverConfig.Env = append(serverConfig.Env, "SHIKI_WORKER_DATA=1")
	worker, e := host.Launch(context.Background(), serverConfig)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { worker.Stop(context.Background()) })
	auth := func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, s connect.ServerStream) error {
			info, ok := connect.CallInfoForServerContext(ctx)
			if !ok || info.RequestHeader().Get("Authorization") != "Bearer shiki_chain_test" {
				return connect.NewError(connect.CodeUnauthenticated, "remote authentication required")
			}
			for _, header := range []string{"shiki-plugin-instance-id", "shiki-host-instance-id", "shiki-server-id"} {
				if info.RequestHeader().Get(header) != "" {
					return connect.NewError(connect.CodePermissionDenied, "local metadata crossed remote boundary")
				}
			}
			return next(ctx, spec, s)
		}
	}
	remote := connect.NewServer(auth)
	rpc.RegisterShikiDaemonHandler(remote, &host.DaemonDataPlane{FileWorker: worker, PortWorker: worker})
	ts := httptest.NewUnstartedServer(host.WithStreamCancellation(connection.NewRPCHandler(remote)))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)
	roots := x509.NewCertPool()
	roots.AddCert(ts.Certificate())
	endpoint, e := connection.NewTLSClient(ts.URL, connection.TLSOptions{RootCAs: roots})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(endpoint.Close)
	token, e := connection.BearerToken("shiki_chain_test")
	if e != nil {
		t.Fatal(e)
	}
	remoteClient := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, token))
	clientConfig, clientProbe := pluginConfig(t, mode)
	clientConfig.Policy.Capabilities = append(clientConfig.Policy.Capabilities, "file.write", "port.forward")
	clientConfig.Core = &chainCore{RemoteCore: &host.RemoteCore{Resolve: func(ctx context.Context, scope *ppb.Scope) (rpc.ShikiDaemonClient, error) {
		if scope.ServerId != "srv-test" {
			return nil, connect.NewError(connect.CodePermissionDenied, "unknown server")
		}
		return remoteClient, nil
	}}, probe: clientProbe}
	local, e := host.Launch(context.Background(), clientConfig)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { local.Stop(context.Background()) })
	return local, serverProbe
}
func TestFullCrossProcessDataPlane(t *testing.T) {
	local, _ := setupChain(t, "chain")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	health, e := local.Health(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if health.Details["shapes"] != "unary,server,client,bidi" || health.Details["final_status"] != "verified" {
		t.Fatalf("data-plane shapes not verified: %v", health)
	}
}
func TestFullChainStreamFinalError(t *testing.T) {
	for _, code := range []connect.Code{connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeAborted, connect.CodeResourceExhausted} {
		t.Run(code.String(), func(t *testing.T) {
			local, _ := setupChain(t, "chain-error-"+code.String())
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_, e := local.Health(ctx)
			if connect.CodeOf(e) != code || strings.Contains(e.Error(), "secret") {
				t.Fatalf("cross-process terminal status: want %v got %v", code, e)
			}
		})
	}
}
func TestFullChainCancellationReachesWorker(t *testing.T) {
	local, probe := setupChain(t, "chain-cancel")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, e := local.Health(ctx); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		probe.mu.Lock()
		source := probe.source
		probe.mu.Unlock()
		if source != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cancellation failed to traverse CoreAPI, remote Connect and native gRPC worker")
}

func TestFullChainUploadAndPortFailure(t *testing.T) {
	for _, mode := range []string{"chain-unary-error", "chain-upload-error", "chain-upload-limit", "chain-port-error"} {
		t.Run(mode, func(t *testing.T) {
			local, _ := setupChain(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_, err := local.Health(ctx)
			want := connect.CodeAborted
			if mode == "chain-upload-limit" {
				want = connect.CodeResourceExhausted
			}
			if connect.CodeOf(err) != want {
				t.Fatalf("%s expected %s, got %v", mode, want, err)
			}
		})
	}
}
func waitWorkerEvent(t *testing.T, p *coreProbe, event string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		latest := p.latestEvent
		p.mu.Unlock()
		if latest == event {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker did not report %s", event)
}
func TestFullChainIdleUploadCancellation(t *testing.T) {
	local, probe := setupChain(t, "chain-upload-wait")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := local.Health(ctx); finished <- err }()
	waitWorkerEvent(t, probe, "worker_upload_open")
	cancel()
	select {
	case err := <-finished:
		if connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatalf("outer cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled upload did not return")
	}
	waitWorkerEvent(t, probe, "worker_canceled")
}
func TestClientCannotRegisterServerDataServices(t *testing.T) {
	config, _ := pluginConfig(t, "")
	config.Env = append(config.Env, "SHIKI_WORKER_DATA=1")
	if instance, err := host.Launch(context.Background(), config); err == nil {
		instance.Stop(context.Background())
		t.Fatal("client plugin registered server data services")
	}
}

func (w *memoryWorker) CheckPort(_ context.Context, r *pb.CheckPortRequest) (*pb.CheckPortResponse, error) {
	return &pb.CheckPortResponse{Port: &pb.OpenPort{Port: r.Port, BindAddress: r.RemoteAddress}}, nil
}
