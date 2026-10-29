package connection

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

type transportProbe struct {
	rpc.UnimplementedShikiDaemonHandler
	canceled   chan struct{}
	requireUID bool
}

func (p *transportProbe) NegotiateProtocol(ctx context.Context, in *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	if p.requireUID {
		if uid, ok := PeerUID(ctx); !ok || uid != uint32(os.Getuid()) {
			return nil, connect.NewError(connect.CodePermissionDenied, "missing peer UID")
		}
	}
	return &pb.ProtocolAgreement{Major: in.Major}, nil
}
func (p *transportProbe) ReadFile(ctx context.Context, in *pb.ReadFileRequest, s rpc.ShikiDaemonReadFileServerStream) error {
	if err := s.Send(&pb.FileData{Payload: &pb.FileData_Chunk{Chunk: []byte("complete bytes")}}); err != nil {
		return err
	}
	if in.Path == "error" {
		return RebuildError(connect.NewError(connect.CodeAborted, "secret downstream text").WithRemote())
	}
	return nil
}
func (p *transportProbe) WriteFile(ctx context.Context, s rpc.ShikiDaemonWriteFileServerStream) (*pb.FileInfo, error) {
	var size int64
	for {
		m, err := s.Receive()
		if errors.Is(err, io.EOF) {
			return &pb.FileInfo{Size: size}, nil
		}
		if err != nil {
			return nil, err
		}
		size += int64(len(m.GetChunk()))
	}
}
func (p *transportProbe) TerminalStream(ctx context.Context, s rpc.ShikiDaemonTerminalStreamServerStream) error {
	m, err := s.Receive()
	if err != nil {
		return err
	}
	if err = s.Send(&pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: []byte("ready")}}); err != nil {
		return err
	}
	if m.GetOpen().GetSessionId() == "cancel" {
		<-ctx.Done()
		close(p.canceled)
		return ctx.Err()
	}
	for {
		_, err = s.Receive()
		if errors.Is(err, io.EOF) {
			return s.Send(&pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: []byte("after EOF")}})
		}
		if err != nil {
			return err
		}
	}
}
func testEndpoint(t testing.TB, kind string, p *transportProbe) *Endpoint {
	t.Helper()
	server := connect.NewServer()
	rpc.RegisterShikiDaemonHandler(server, p)
	handler := NewRPCHandler(server)
	if kind == "tls" {
		ts := httptest.NewUnstartedServer(handler)
		ts.EnableHTTP2 = true
		ts.StartTLS()
		t.Cleanup(ts.Close)
		roots := x509.NewCertPool()
		roots.AddCert(ts.Certificate())
		endpoint, err := NewTLSClient(ts.URL, TLSOptions{RootCAs: roots})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(endpoint.Close)
		return endpoint
	}
	p.requireUID = true
	dir, err := os.MkdirTemp("/tmp", "shiki-transport-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "rpc.sock")
	listener, err := ListenUnix(socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hs := NewHTTPServer(ctx, handler)
	done := make(chan error, 1)
	go func() { done <- hs.Serve(listener) }()
	t.Cleanup(func() { cancel(); hs.Close(); <-done })
	endpoint, err := NewUnixClient(socket, uint32(os.Getuid()), "shiki-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(endpoint.Close)
	return endpoint
}
func TestFourRPCShapes(t *testing.T) {
	for _, kind := range []string{"unix", "tls"} {
		t.Run(kind, func(t *testing.T) {
			p := &transportProbe{canceled: make(chan struct{})}
			endpoint := testEndpoint(t, kind, p)
			client := rpc.NewShikiDaemonClient(NewRPCClient(endpoint))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			answer, err := client.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1})
			if err != nil || answer.Major != 1 {
				t.Fatalf("unary: %v %v", answer, err)
			}
			read, err := client.ReadFile(ctx, &pb.ReadFileRequest{})
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			data, err := read.Receive()
			if err != nil || string(data.GetChunk()) != "complete bytes" {
				t.Fatalf("server stream: %v %v", data, err)
			}
			if _, err = read.Receive(); !errors.Is(err, io.EOF) {
				t.Fatalf("terminal status: %v", err)
			}
			write, err := client.WriteFile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if err = write.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: []byte("abc")}}); err != nil {
					t.Fatal(err)
				}
			}
			info, err := write.CloseAndReceive()
			if err != nil || info.GetSize() != 9 {
				t.Fatalf("client stream: %v %v", info, err)
			}
			bidi, err := client.TerminalStream(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer bidi.Close()
			if err = bidi.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "test"}}}); err != nil {
				t.Fatal(err)
			}
			ready, err := bidi.Receive()
			if err != nil || string(ready.GetData()) != "ready" {
				t.Fatalf("response before CloseSend: %v %v", ready, err)
			}
			if err = bidi.CloseSend(); err != nil {
				t.Fatal(err)
			}
			last, err := bidi.Receive()
			if err != nil || string(last.GetData()) != "after EOF" {
				t.Fatalf("half close: %v %v", last, err)
			}
			if _, err = bidi.Receive(); !errors.Is(err, io.EOF) {
				t.Fatalf("bidi final status: %v", err)
			}
			failed, err := client.ReadFile(ctx, &pb.ReadFileRequest{Path: "error"})
			if err != nil {
				t.Fatal(err)
			}
			defer failed.Close()
			if _, err = failed.Receive(); err != nil {
				t.Fatal(err)
			}
			_, err = failed.Receive()
			if connect.CodeOf(err) != connect.CodeAborted || strings.Contains(err.Error(), "secret") {
				t.Fatalf("downstream error: %v", err)
			}
			child, stop := context.WithCancel(ctx)
			idle, err := client.TerminalStream(child)
			if err != nil {
				t.Fatal(err)
			}
			defer idle.Close()
			if err = idle.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "cancel"}}}); err != nil {
				t.Fatal(err)
			}
			if _, err = idle.Receive(); err != nil {
				t.Fatal(err)
			}
			stop()
			if _, err = idle.Receive(); connect.CodeOf(err) != connect.CodeCanceled {
				t.Fatalf("cancel receive: %v", err)
			}
			select {
			case <-p.canceled:
			case <-ctx.Done():
				t.Fatal("server did not observe cancellation")
			}
		})
	}
}
func TestProtocolAdmission(t *testing.T) {
	endpoint := testEndpoint(t, "unix", &transportProbe{})
	cases := []struct {
		name, method, content string
		versions              []string
		status                int
	}{
		{"json", "POST", "application/json", []string{"1"}, 415},
		{"grpc", "POST", "application/grpc+proto", []string{"1"}, 415},
		{"grpcweb", "POST", "application/grpc-web+proto", []string{"1"}, 415},
		{"get", "GET", "application/proto", []string{"1"}, 405},
		{"missing-version", "POST", "application/proto", nil, 400},
		{"duplicate-version", "POST", "application/proto", []string{"1", "1"}, 400},
		{"wrong-shape", "POST", "application/connect+proto", []string{"1"}, 415},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, endpoint.BaseURL+rpc.ShikiDaemonNegotiateProtocolProcedure, bytes.NewReader(nil))
			req.Header.Set("Content-Type", tc.content)
			req.Header["Connect-Protocol-Version"] = tc.versions
			res, err := endpoint.HTTPClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status || res.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("status %d headers %v", res.StatusCode, res.Header)
			}
		})
	}
	// Exercise admission independently because an HTTP/2-only listener itself
	// may close a cleartext HTTP/1 connection before invoking a handler.
	r := httptest.NewRequest("POST", "http://local", nil)
	w := httptest.NewRecorder()
	NewRPCHandler(connect.NewServer()).ServeHTTP(w, r)
	if w.Code != 505 {
		t.Fatalf("HTTP1 accepted: %d", w.Code)
	}
	req, _ := http.NewRequest("POST", "http://attacker.invalid/", nil)
	if _, err := endpoint.HTTPClient.Do(req); err == nil {
		t.Fatal("accepted arbitrary endpoint")
	}
}
func TestLimitsAndCompression(t *testing.T) {
	endpoint := testEndpoint(t, "unix", &transportProbe{})
	client := rpc.NewShikiDaemonClient(NewRPCClient(endpoint))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.WriteFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: make([]byte, MaxMessageBytes+1)}})
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("oversize send: %v", err)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint.BaseURL+rpc.ShikiDaemonNegotiateProtocolProcedure, bytes.NewReader([]byte{1, 2, 3}))
	req.Header.Set("Content-Type", "application/proto")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Content-Encoding", "gzip")
	res, err := endpoint.HTTPClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode < 400 {
		t.Fatal("compressed request accepted")
	}
}
func TestTLSValidationAndRedirects(t *testing.T) {
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://attacker.invalid/", 302) }))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()
	endpoint, err := NewTLSClient(ts.URL, TLSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	if _, err = endpoint.HTTPClient.Get(ts.URL); err == nil || IsRetryableNetworkError(err) {
		t.Fatalf("certificate failure: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ts.Certificate())
	trusted, err := NewTLSClient(ts.URL, TLSOptions{RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer trusted.Close()
	res, err := trusted.HTTPClient.Get(ts.URL)
	if res != nil {
		res.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "redirects are disabled") {
		t.Fatalf("redirect: %v", err)
	}
	if _, err = NewTLSClient("http://localhost", TLSOptions{}); err == nil {
		t.Fatal("plaintext accepted")
	}
	// A valid certificate alone is insufficient: ALPN must select h2.
	h1 := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer h1.Close()
	roots.AddCert(h1.Certificate())
	one, err := NewTLSClient(h1.URL, TLSOptions{RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	if _, err = one.HTTPClient.Get(h1.URL); err == nil {
		t.Fatal("HTTP/1 TLS endpoint accepted")
	}
}
func TestUnixOwnership(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "shiki-uid-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "sock")
	l, err := ListenUnix(path, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err = ListenUnix(path, uint32(os.Getuid())); err == nil {
		t.Fatal("overwrote live socket")
	}
	if _, err = NewUnixClient(path, uint32(os.Getuid()+1), "local"); err == nil {
		t.Fatal("wrong owner accepted")
	}
	if err = os.Chmod(path, 0660); err != nil {
		t.Fatal(err)
	}
	if _, err = NewUnixClient(path, uint32(os.Getuid()), "local"); err == nil {
		t.Fatal("permissive socket accepted")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = NewUnixClient(path, uint32(os.Getuid()), "local"); err == nil {
		t.Fatal("permissive directory accepted")
	}
}
func TestRetryPolicy(t *testing.T) {
	count := 0
	err := retryConnect(context.Background(), func(context.Context) error {
		count++
		return &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("network unavailable")}
	}, func(int) time.Duration { return 0 })
	if err == nil || count != 6 {
		t.Fatalf("attempts %d, error %v", count, err)
	}
	for _, code := range []connect.Code{connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeUnavailable} {
		count = 0
		_ = retryConnect(context.Background(), func(context.Context) error { count++; return connect.NewError(code, "no").WithRemote() }, func(int) time.Duration { return 0 })
		if count != 1 {
			t.Fatalf("remote %v retried", code)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	count = 0
	err = retryConnect(ctx, func(context.Context) error {
		count++
		cancel()
		return &net.OpError{Op: "dial", Err: errors.New("network")}
	}, func(int) time.Duration { return time.Hour })
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatalf("cancel: %v attempts %d", err, count)
	}
}

func TestServerReceiveLimit(t *testing.T) {
	endpoint := testEndpoint(t, "unix", &transportProbe{})
	// Bypass the normal client's smaller send limit to exercise the server's
	// independent defense, not merely a client-side validation failure.
	client := rpc.NewShikiDaemonClient(connect.NewClient(connecthttp.NewTransport(endpoint.HTTPClient, endpoint.BaseURL, connecthttp.WithCompressors(), connecthttp.WithSendMaxBytes(2*MaxMessageBytes))))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.WriteFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: make([]byte, MaxMessageBytes+1)}})
	_, err = stream.CloseAndReceive()
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("server receive limit: %v", err)
	}
}

func TestMissingEndStreamFails(t *testing.T) {
	message, err := proto.Marshal(&pb.FileData{Payload: &pb.FileData_Chunk{Chunk: []byte("looks complete")}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/connect+proto")
		frame := make([]byte, 5+len(message))
		binary.BigEndian.PutUint32(frame[1:5], uint32(len(message)))
		copy(frame[5:], message)
		w.Write(frame) // Deliberately omit Connect EndStream: HTTP 200 is not success.
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()
	roots := x509.NewCertPool()
	roots.AddCert(ts.Certificate())
	endpoint, err := NewTLSClient(ts.URL, TLSOptions{RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := rpc.NewShikiDaemonClient(NewRPCClient(endpoint)).ReadFile(ctx, &pb.ReadFileRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err = stream.Receive(); err != nil {
		t.Fatalf("first message: %v", err)
	}
	if _, err = stream.Receive(); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("truncated stream accepted: %v", err)
	}
}

func TestRemoteErrorCodesAreRebuilt(t *testing.T) {
	for _, code := range []connect.Code{connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeAborted, connect.CodeResourceExhausted} {
		upstream := connect.NewError(code, "secret token and path").WithRemote()
		err := RebuildError(upstream)
		var actual *connect.Error
		if !errors.As(err, &actual) || actual.IsRemote() || actual.Code() != code || strings.Contains(actual.Message(), "secret") {
			t.Fatalf("rebuild %v: %v", code, err)
		}
	}
	if IsRetryableNetworkError(ErrHTTP2Required) {
		t.Fatal("HTTP/2 protocol violation retried")
	}
}
