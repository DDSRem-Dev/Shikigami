package plugin

import (
	"context"
	"crypto/x509"
	"io"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
)

type idleBrowserUpload struct {
	ctx        context.Context
	fail       <-chan struct{}
	final      error
	receiving  chan struct{}
	sent       chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once
	halfClosed atomic.Bool
}

func (s *idleBrowserUpload) Send(*epb.BrowserFileChunk) error {
	select {
	case s.sent <- struct{}{}:
	default:
	}
	return nil
}
func (s *idleBrowserUpload) CloseSend() error { s.halfClosed.Store(true); return nil }
func (s *idleBrowserUpload) Receive() (*epb.BrowserFile, error) {
	close(s.receiving)
	select {
	case <-s.fail:
		return nil, s.final
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}
func (s *idleBrowserUpload) CloseAndReceive() (*epb.BrowserFile, error) {
	if err := s.CloseSend(); err != nil {
		return nil, err
	}
	return s.Receive()
}
func (s *idleBrowserUpload) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

type uploadBrowserClient struct {
	sdk.BrowserClient
	stream *idleBrowserUpload
}

func (c uploadBrowserClient) WriteBrowserFile(ctx context.Context) (sdk.BrowserWriteStream, error) {
	c.stream.ctx = ctx
	return c.stream, nil
}

type uploadLifecycle struct {
	sdk.Lifecycle
	client sdk.BrowserClient
}

func (l uploadLifecycle) Browser() sdk.BrowserClient         { return l.client }
func (uploadLifecycle) Region() sdk.RegionClient             { return nil }
func (uploadLifecycle) BrowserLocal() sdk.BrowserLocalClient { return nil }

type uploadDaemonHandler struct {
	erpc.UnimplementedBrowserServiceHandler
	data *DaemonDataPlane
	done chan struct{}
}

func (h uploadDaemonHandler) WriteBrowserFile(ctx context.Context, stream erpc.BrowserServiceWriteBrowserFileServerStream) (*epb.BrowserFile, error) {
	defer close(h.done)
	return h.data.WriteBrowserFile(ctx, stream)
}

// Exercise the consumer-visible Connect endpoint, not only a path echo or an
// already-half-closed upload. The sender deliberately stays idle after headers
// or one frame; only the downstream worker's terminal status ends the call.
func TestDaemonBrowserUploadIdleSenderReceivesWorkerFailure(t *testing.T) {
	for _, firstFrame := range []bool{false, true} {
		for _, code := range []connect.Code{connect.CodeDeadlineExceeded, connect.CodeResourceExhausted, connect.CodeCanceled} {
			t.Run(code.String()+map[bool]string{false: "/before-first-frame", true: "/after-first-frame"}[firstFrame], func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				fail := make(chan struct{})
				stream := &idleBrowserUpload{fail: fail, final: connect.NewError(code, "worker final failure"), receiving: make(chan struct{}), sent: make(chan struct{}, 1), closed: make(chan struct{})}
				store, err := storage.Open(filepath.Join(t.TempDir(), "intent"), storage.Options{DisableRecovery: true})
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				want := desiredPlugin{ID: "browser", Enabled: true, Revision: "rev_test"}
				if err := store.Update(func(state *storage.Snapshot) error { return state.Put("plugins", "browser", want) }); err != nil {
					t.Fatal(err)
				}
				instance := &Instance{
					grant: &grant{valid: true, lifetime: ctx, deadline: time.Now().Add(time.Hour)}, done: make(chan struct{}),
					lifecycle: uploadLifecycle{client: uploadBrowserClient{stream: stream}},
					snapshot:  Snapshot{State: "ready", Protocol: &pb.ProtocolAgreement{Capabilities: []string{sdk.BrowserCapability}}},
				}
				manager := &Supervisor{store: store, slots: map[string]*supervised{"browser": {status: &pb.PluginStatus{State: "ready", Revision: want.Revision}, instance: instance}}}
				done := make(chan struct{})
				server := connect.NewServer()
				erpc.RegisterBrowserServiceHandler(server, uploadDaemonHandler{data: &DaemonDataPlane{Supervisor: manager}, done: done})
				remote := httptest.NewUnstartedServer(connection.NewRPCHandler(server, WithStreamCancellation))
				remote.EnableHTTP2 = true
				remote.StartTLS()
				defer remote.Close()
				roots := x509.NewCertPool()
				roots.AddCert(remote.Certificate())
				endpoint, err := connection.NewTLSClient(remote.URL, connection.TLSOptions{RootCAs: roots})
				if err != nil {
					t.Fatal(err)
				}
				defer endpoint.Close()
				client := connection.NewRPCClient(endpoint)
				upstream, err := client.CallClientStream(ctx, connect.Spec{StreamType: connect.StreamTypeClient, Schema: epb.File_extensions_proto.Services().ByName("BrowserService").Methods().ByName("WriteBrowserFile"), Procedure: erpc.BrowserServiceWriteBrowserFileProcedure})
				if err != nil {
					t.Fatal(err)
				}
				defer upstream.Close()
				if err := upstream.SendHeaders(); err != nil {
					t.Fatal(err)
				}
				result := make(chan error, 1)
				go func() { result <- upstream.Receive(new(epb.BrowserFile)) }()
				select {
				case <-stream.receiving:
				case <-ctx.Done():
					t.Fatal("daemon did not concurrently observe the worker's result")
				}
				if firstFrame {
					if err := upstream.Send(&epb.BrowserFileChunk{ProfileId: "profile", Name: "upload.txt"}); err != nil {
						t.Fatal(err)
					}
					select {
					case <-stream.sent:
					case <-ctx.Done():
						t.Fatal("upload first frame did not reach worker")
					}
				}
				close(fail)
				select {
				case err := <-result:
					if connect.CodeOf(err) != code {
						t.Fatalf("worker terminal error became %v; want %v", err, code)
					}
				case <-ctx.Done():
					t.Fatal("idle uploader retained the daemon after worker failure")
				}
				select {
				case <-done:
				case <-ctx.Done():
					t.Fatal("daemon returned a response without draining the upload sender")
				}
				select {
				case <-stream.closed:
				default:
					t.Fatal("worker upload resources were not closed")
				}
				if stream.halfClosed.Load() {
					t.Fatal("terminal observation half-closed the uploader before failure")
				}
			})
		}
	}
}

func TestBrowserUploadBridgeJoinsBlockedReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, released, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	ctx = context.WithValue(ctx, cancelReadKey{}, func() { once.Do(func() { close(released) }) })
	fail := make(chan struct{})
	stream := &idleBrowserUpload{ctx: ctx, fail: fail, final: connect.NewError(connect.CodeDeadlineExceeded, "worker upload deadline"), receiving: make(chan struct{}), closed: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		_, err := bridgeBrowserUpload(ctx, cancel, func() (*epb.BrowserFileChunk, error) {
			close(entered)
			<-released
			defer close(exited)
			return nil, io.ErrClosedPipe
		}, stream)
		result <- err
	}()
	<-entered
	close(fail)
	select {
	case err := <-result:
		if connect.CodeOf(err) != connect.CodeDeadlineExceeded {
			t.Fatal("upstream shutdown replaced the worker deadline", err)
		}
		select {
		case <-exited:
		default:
			t.Fatal("bridge returned before the blocked upstream reader exited")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker deadline did not interrupt blocked upstream Receive")
	}
}
