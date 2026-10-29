package hostrotation_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// This fixture terminates TLS and opens a separately verified TLS/HTTP2 hop to
// the real daemon. It does not rewrite RPC payloads, application credentials or
// domain errors; the standard reverse proxy handles transport headers.
// Only the selected file stream is gated; identity, stats and lifecycle RPCs
// remain live, so natural plugin expiry is not confused with route failure.
type fileTransferGate struct {
	armed         atomic.Bool
	claimed       atomic.Bool
	path          string
	oldOpen       chan struct{}
	newOpen       chan struct{}
	oldDone       chan struct{}
	oldOnce       sync.Once
	newOnce       sync.Once
	bytes         atomic.Int64
	ended         atomic.Int64
	canceledAtEnd atomic.Bool
	server        *httptest.Server
}

func newFileTransferGate(t *testing.T, target, certPath, keyPath, direction string) *fileTransferGate {
	t.Helper()
	g := &fileTransferGate{oldOpen: make(chan struct{}), newOpen: make(chan struct{}), oldDone: make(chan struct{})}
	g.path = rpc.ShikiDaemonWriteFileProcedure
	if direction == "download" {
		g.path = rpc.ShikiDaemonReadFileProcedure
	}
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("fixture daemon CA could not be parsed")
	}
	transport := &http.Transport{ForceAttemptHTTP2: true, TLSHandshakeTimeout: 5 * time.Second, TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}}
	upstream, err := url.Parse("https://" + target)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = &gateProbeTrace{base: transport, t: t}
	proxy.FlushInterval = -1
	proxy.ErrorLog = log.New(io.Discard, "", 0) // Never log forwarded credentials or URL details.
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.ProtoMajor != 2 {
			return errors.New("fixture upstream did not negotiate HTTP/2")
		}
		if first, _ := response.Request.Context().Value(fileGateKey{}).(bool); first && direction == "download" {
			response.Body = g.body(response.Request.Context(), response.Body)
		}
		return nil
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			http.Error(w, "HTTP/2 required", http.StatusHTTPVersionNotSupported)
			return
		}
		if r.URL.Path != g.path || !g.armed.Load() {
			proxy.ServeHTTP(w, r)
			return
		}
		first := g.claimed.CompareAndSwap(false, true)
		if first {
			defer func() {
				g.canceledAtEnd.Store(r.Context().Err() != nil)
				g.ended.Store(time.Now().UnixNano())
				close(g.oldDone)
			}()
			r = r.WithContext(context.WithValue(r.Context(), fileGateKey{}, true))
			if direction == "upload" {
				r.Body = g.body(r.Context(), r.Body)
			}
		} else {
			// A replacement transfer cannot obscure cleanup of the old partial.
			select {
			case <-r.Context().Done():
				return
			case <-g.newOpen:
			}
		}
		proxy.ServeHTTP(w, r)
	})
	g.server = httptest.NewUnstartedServer(handler)
	g.server.EnableHTTP2 = true
	// A deliberately unread file stream must not consume the whole relay's
	// connection receive credit and starve unrelated unary request bodies.
	// This bounded allowance is private to the fixture, not a host setting.
	g.server.Config.HTTP2 = &http.HTTP2Config{MaxReceiveBufferPerConnection: 8 << 20, MaxReceiveBufferPerStream: 1 << 20}
	g.server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	g.server.StartTLS()
	t.Cleanup(func() {
		g.releaseOld()
		g.releaseNew()
		g.server.CloseClientConnections()
		g.server.Close()
		transport.CloseIdleConnections()
	})
	return g
}

type fileGateKey struct{}

func (g *fileTransferGate) releaseOld() { g.oldOnce.Do(func() { close(g.oldOpen) }) }
func (g *fileTransferGate) releaseNew() { g.newOnce.Do(func() { close(g.newOpen) }) }

// The first 768 KiB includes multiple complete 256 KiB protobuf chunks. The
// remaining bytes cannot enter the destination before the natural expiry.
func (g *fileTransferGate) body(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	return &gatedFileBody{ReadCloser: body, ctx: ctx, gate: g, remaining: 768 << 10, closed: make(chan struct{})}
}

type gatedFileBody struct {
	io.ReadCloser
	ctx       context.Context
	gate      *fileTransferGate
	remaining int
	closed    chan struct{}
	once      sync.Once
}

func (b *gatedFileBody) Read(buffer []byte) (int, error) {
	if b.remaining == 0 {
		select {
		case <-b.ctx.Done():
			return 0, b.ctx.Err()
		case <-b.closed:
			return 0, context.Canceled
		case <-b.gate.oldOpen:
		}
	} else if len(buffer) > b.remaining {
		buffer = buffer[:b.remaining]
	}
	n, err := b.ReadCloser.Read(buffer)
	if b.remaining > 0 {
		b.remaining -= n
	}
	b.gate.bytes.Add(int64(n))
	return n, err
}

func (b *gatedFileBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return b.ReadCloser.Close()
}

type gateProbeTrace struct {
	base http.RoundTripper
	t    *testing.T
}

func (p *gateProbeTrace) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path != rpc.ShikiDaemonNegotiateProtocolProcedure {
		return p.base.RoundTrip(r)
	}
	start := time.Now()
	count := &gateProbeBody{ReadCloser: r.Body}
	r.Body = count
	response, err := p.base.RoundTrip(r)
	if time.Since(start) > time.Second || err != nil {
		p.t.Logf("relay negotiation: started=%s elapsed=%s declared_request_bytes=%d forwarded_request_bytes=%d caller_context=%v upstream_error_type=%T", start.Format(time.RFC3339Nano), time.Since(start), r.ContentLength, count.bytes.Load(), r.Context().Err(), err)
	}
	return response, err
}

type gateProbeBody struct {
	io.ReadCloser
	bytes atomic.Int64
}

func (b *gateProbeBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.bytes.Add(int64(n))
	return n, err
}
