package server

import (
	"bytes"
	"context"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type admissionFixture struct {
	service *Service
	auth    *auth.Manager
	remote  *httptest.Server
	arrived chan string
	mu      sync.Mutex
	peers   map[string]bool
}

func newAdmissionFixture(t *testing.T) *admissionFixture {
	t.Helper()
	manager, err := auth.Open(filepath.Join(t.TempDir(), "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	f := &admissionFixture{auth: manager, arrived: make(chan string, 256), peers: map[string]bool{}}
	f.service = &Service{Auth: manager}
	handler := f.service.Handler()
	f.remote = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.peers[r.RemoteAddr] = true
		f.mu.Unlock()
		select {
		case f.arrived <- r.URL.Path:
		default:
		}
		handler.ServeHTTP(w, r)
	}))
	f.remote.EnableHTTP2 = true
	f.remote.StartTLS()
	t.Cleanup(f.remote.Close)
	return f
}

func (f *admissionFixture) client(t *testing.T, permissions ...string) *connect.Client {
	t.Helper()
	token, _, err := f.auth.Create("tester", permissions, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(f.remote.Certificate())
	endpoint, err := connection.NewTLSClient(f.remote.URL, connection.TLSOptions{RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(endpoint.Close)
	credential, err := connection.BearerToken(token)
	if err != nil {
		t.Fatal(err)
	}
	return connection.NewRPCClient(endpoint, credential)
}

type silentAdmissionCall struct {
	cancel context.CancelFunc
	writer *io.PipeWriter
	done   chan struct{}
	status int
	body   []byte
	err    error
}

func (f *admissionFixture) silentRequest(t *testing.T, parent context.Context, method, timeout string) *silentAdmissionCall {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	reader, writer := io.Pipe()
	path := "/shiki.v1.ShikiDaemon/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.remote.URL+path, reader)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	token, _, err := f.auth.Create("tester", []string{"*"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Connect-Protocol-Version", "1")
	request.Header.Set("Content-Type", "application/connect+proto")
	if method == "ListFiles" || method == "StatFile" || method == "DeleteFile" || method == "ListPorts" || method == "CheckPort" {
		request.Header.Set("Content-Type", "application/proto")
	}
	if timeout != "" {
		request.Header.Set("Connect-Timeout-Ms", timeout)
	}
	call := &silentAdmissionCall{cancel: cancel, writer: writer, done: make(chan struct{})}
	go func() {
		defer close(call.done)
		response, err := f.remote.Client().Do(request)
		if err != nil {
			call.err = err
			return
		}
		defer response.Body.Close()
		call.status = response.StatusCode
		call.body, call.err = io.ReadAll(io.LimitReader(response.Body, 64<<10))
	}()
	t.Cleanup(func() {
		cancel()
		_ = writer.Close()
		select {
		case <-call.done:
		case <-time.After(time.Second):
			t.Error("HTTP request helper did not exit")
		}
	})
	select {
	case arrived := <-f.arrived:
		if arrived != path {
			t.Fatal("unexpected arrival", arrived)
		}
	case <-time.After(time.Second):
		t.Fatal("silent HTTP2 headers did not reach server")
	}
	return call
}

func TestFileAdmissionSharesTransferBudgetAcrossConnections(t *testing.T) {
	f := newAdmissionFixture(t)
	second := f.client(t, "*")
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	var pending []*silentAdmissionCall
	for range 16 {
		pending = append(pending, f.silentRequest(t, ctx, "ReadFile", ""))
	}
	for _, call := range pending {
		select {
		case <-call.done:
			t.Fatal("silent first-frame request already ended", call.err, string(call.body))
		default:
		}
	}
	// Each authenticated stream has reached the generated adapter's blocked
	// Receive; no worker is needed to reproduce host admission before decode.
	var observed error
	until := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(until) {
		stream, err := rpc.NewShikiDaemonClient(second).ReadFile(ctx, &pb.ReadFileRequest{})
		if err == nil {
			_, err = stream.Receive()
			_ = stream.Close()
		}
		observed = err
		if connect.CodeOf(err) == connect.CodeResourceExhausted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if connect.CodeOf(observed) != connect.CodeResourceExhausted {
		t.Fatalf("17th file transfer on a second connection/token bypassed shared budget: %v", observed)
	}
	f.mu.Lock()
	connections := len(f.peers)
	f.mu.Unlock()
	if connections < 2 {
		t.Fatal("test did not open independent HTTP2 connections")
	}
}

func waitFileCount(t *testing.T, counter *atomic.Int32, want int32) {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		if counter.Load() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("file admission count %d, wanted %d", counter.Load(), want)
}

func probeFile(t *testing.T, client *connect.Client, ctx context.Context, method string) error {
	t.Helper()
	api := rpc.NewShikiDaemonClient(client)
	switch method {
	case "ListFiles":
		_, err := api.ListFiles(ctx, &pb.ListFilesRequest{})
		return err
	case "StatFile":
		_, err := api.StatFile(ctx, &pb.StatFileRequest{})
		return err
	case "DeleteFile":
		_, err := api.DeleteFile(ctx, &pb.DeleteFileRequest{})
		return err
	case "ReadFile":
		stream, err := api.ReadFile(ctx, &pb.ReadFileRequest{})
		if err != nil {
			return err
		}
		defer stream.Close()
		_, err = stream.Receive()
		return err
	case "WriteFile":
		stream, err := api.WriteFile(ctx)
		if err != nil {
			return err
		}
		_, err = stream.CloseAndReceive()
		return err
	case "WatchFiles":
		stream, err := api.WatchFiles(ctx, &pb.WatchFilesRequest{})
		if err != nil {
			return err
		}
		defer stream.Close()
		_, err = stream.Receive()
		return err
	}
	t.Fatal("unknown probe method")
	return nil
}

func TestFileAdmissionCategoriesAreIndependentAndReclaimed(t *testing.T) {
	for _, method := range []string{"ListFiles", "ReadFile", "WatchFiles"} {
		t.Run(method, func(t *testing.T) {
			f := newAdmissionFixture(t)
			client := f.client(t, "*")
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			counter, maximum := f.service.fileCalls.budget("/shiki.v1.ShikiDaemon/" + method)
			var held []*silentAdmissionCall
			for range maximum {
				held = append(held, f.silentRequest(t, ctx, method, ""))
			}
			waitFileCount(t, counter, maximum)
			for _, probe := range []string{"ListFiles", "StatFile", "DeleteFile", "ReadFile", "WriteFile", "WatchFiles"} {
				other, _ := f.service.fileCalls.budget("/shiki.v1.ShikiDaemon/" + probe)
				want := connect.CodeFailedPrecondition // admitted, but no plugin fixture
				if other == counter {
					want = connect.CodeResourceExhausted
				}
				if err := probeFile(t, client, ctx, probe); connect.CodeOf(err) != want {
					t.Fatalf("%s under saturated %s: %v", probe, method, err)
				}
			}
			terminal, err := rpc.NewShikiDaemonClient(client).TerminalStream(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer terminal.Close()
			if err = terminal.Send(&pb.TerminalInput{}); err != nil {
				t.Fatal(err)
			}
			if _, err = terminal.Receive(); connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("file budget blocked independent terminal admission: %v", err)
			}
			for _, call := range held {
				call.cancel()
				_ = call.writer.Close()
			}
			waitFileCount(t, counter, 0)
			if err := probeFile(t, client, ctx, method); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatalf("released file slot could not be reused: %v", err)
			}
		})
	}
}

func TestFileAdmissionAuthorizationBeforeBudgetAndSharedHandlers(t *testing.T) {
	f := newAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	for range limits.FileTransfers {
		f.silentRequest(t, ctx, "ReadFile", "")
	}
	waitFileCount(t, &f.service.fileCalls.transfers, limits.FileTransfers)
	denied := f.client(t, "system.info")
	if err := probeFile(t, denied, ctx, "ReadFile"); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("budget revealed before authorization: %v", err)
	}
	waitFileCount(t, &f.service.fileCalls.transfers, limits.FileTransfers)
	other := httptest.NewUnstartedServer(f.service.Handler())
	other.EnableHTTP2 = true
	other.StartTLS()
	defer other.Close()
	secondFixture := &admissionFixture{auth: f.auth, service: f.service, remote: other}
	second := secondFixture.client(t, "*")
	if err := probeFile(t, second, ctx, "ReadFile"); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("constructing a second Handler reset the Service budget: %v", err)
	}
	if _, err := rpc.NewShikiDaemonClient(denied).HealthCheck(ctx, &pb.Empty{}); err != nil {
		t.Fatalf("file budget blocked independent health call: %v", err)
	}
}

func TestFileAdmissionSilentFirstFrameDeadlineAndCancellation(t *testing.T) {
	t.Run("default-five-seconds", func(t *testing.T) {
		f := newAdmissionFixture(t)
		started := time.Now()
		call := f.silentRequest(t, t.Context(), "ReadFile", "")
		waitFileCount(t, &f.service.fileCalls.transfers, 1)
		select {
		case <-call.done:
		case <-time.After(7 * time.Second):
			t.Fatal("first-frame Receive did not unblock")
		}
		if elapsed := time.Since(started); elapsed < limits.FileFirstFrameTimeout || elapsed > 7*time.Second {
			t.Fatal("wrong default first-frame timeout", elapsed)
		}
		if call.err != nil || !bytes.Contains(call.body, []byte("deadline_exceeded")) {
			t.Fatalf("timeout did not return a Connect deadline: %v %q", call.err, call.body)
		}
		waitFileCount(t, &f.service.fileCalls.transfers, 0)
		client := f.client(t, "*")
		if err := probeFile(t, client, t.Context(), "ReadFile"); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatal("timeout retained admission slot", err)
		}
	})
	t.Run("earlier-wire-parent-deadline", func(t *testing.T) {
		f := newAdmissionFixture(t)
		started := time.Now()
		// The HTTP client stays live; only Connect's server-side derived
		// deadline expires. Auth's outer request context is not that child.
		call := f.silentRequest(t, t.Context(), "WatchFiles", "120")
		select {
		case <-call.done:
		case <-time.After(time.Second):
			t.Fatal("earlier Connect deadline left Receive blocked")
		}
		if time.Since(started) > time.Second {
			t.Fatal("parent deadline extended")
		}
		if call.err != nil || !bytes.Contains(call.body, []byte("deadline_exceeded")) {
			t.Fatalf("earlier deadline result: %v %q", call.err, call.body)
		}
		waitFileCount(t, &f.service.fileCalls.watches, 0)
	})
	t.Run("client-cancellation", func(t *testing.T) {
		f := newAdmissionFixture(t)
		call := f.silentRequest(t, t.Context(), "ReadFile", "")
		waitFileCount(t, &f.service.fileCalls.transfers, 1)
		call.cancel()
		waitFileCount(t, &f.service.fileCalls.transfers, 0)
		select {
		case <-call.done:
		case <-time.After(time.Second):
			t.Fatal("canceled HTTP client did not join")
		}
	})
}
