package syncack_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

// ackGate captures a complete successful upstream response, but never writes
// any of its ACK body to the client. Only the selected HTTP/2 stream is reset.
// Subsequent file operations wait separately so recovery cannot conceal an
// uncertain baseline or race the test's external edit.
type ackGate struct {
	armed                atomic.Bool
	claimed              atomic.Bool
	committed            chan commitObservation
	drop                 chan struct{}
	dropped              chan struct{}
	resume               chan struct{}
	blocked              chan string
	dropOnce, resumeOnce sync.Once
	writes               atomic.Int64
	ackBytes             atomic.Int64
	mu                   sync.Mutex
	recovery             []string
	server               *httptest.Server
}
type commitObservation struct {
	info      *pb.FileInfo
	wireBytes int
	err       error
}
type selectedACK struct{}

func newACKGate(t *testing.T, target, certPath, keyPath string) *ackGate {
	t.Helper()
	g := &ackGate{committed: make(chan commitObservation, 1), drop: make(chan struct{}), dropped: make(chan struct{}), resume: make(chan struct{}), blocked: make(chan string, 32)}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("invalid CA")
	}
	transport := &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}}
	targetURL, err := url.Parse("https://" + target)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ModifyResponse = func(r *http.Response) error {
		if r.ProtoMajor != 2 {
			return errors.New("upstream did not use HTTP/2")
		}
		if selected, _ := r.Request.Context().Value(selectedACK{}).(bool); !selected {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		r.Body.Close()
		var info *pb.FileInfo
		if err == nil {
			info, err = decodeCommit(r, data)
		}
		g.committed <- commitObservation{info: info, wireBytes: len(data), err: err}
		select {
		case <-g.drop:
		case <-r.Request.Context().Done():
			return r.Request.Context().Err()
		}
		// ModifyResponse executes before headers or the response body are forwarded.
		// ErrorHandler aborts this stream instead of generating a replacement ACK.
		return errors.New("fixture intentionally lost the committed ACK")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if selected, _ := r.Context().Value(selectedACK{}).(bool); selected {
			panic(http.ErrAbortHandler)
		}
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			http.Error(w, "HTTP/2 required", http.StatusHTTPVersionNotSupported)
			return
		}
		selected := g.armed.Load() && r.URL.Path == rpc.ShikiDaemonWriteFileProcedure && g.claimed.CompareAndSwap(false, true)
		if selected {
			g.writes.Add(1)
			defer close(g.dropped)
			r = r.WithContext(context.WithValue(r.Context(), selectedACK{}, true))
			proxy.ServeHTTP(&countACKWriter{ResponseWriter: w, n: &g.ackBytes}, r)
			return
		}
		if g.claimed.Load() && isRecoveryFile(r.URL.Path) {
			select {
			case g.blocked <- r.URL.Path:
			default:
			}
			select {
			case <-r.Context().Done():
				return
			case <-g.resume:
			}
			g.mu.Lock()
			g.recovery = append(g.recovery, r.URL.Path)
			g.mu.Unlock()
			if r.URL.Path == rpc.ShikiDaemonWriteFileProcedure {
				g.writes.Add(1)
			}
		}
		proxy.ServeHTTP(w, r)
	})
	g.server = httptest.NewUnstartedServer(handler)
	g.server.EnableHTTP2 = true
	g.server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	g.server.StartTLS()
	t.Cleanup(func() {
		g.releaseACK()
		g.releaseRecovery()
		g.server.CloseClientConnections()
		g.server.Close()
		transport.CloseIdleConnections()
	})
	return g
}
func (g *ackGate) releaseACK()      { g.dropOnce.Do(func() { close(g.drop) }) }
func (g *ackGate) releaseRecovery() { g.resumeOnce.Do(func() { close(g.resume) }) }
func isRecoveryFile(path string) bool {
	switch path {
	case rpc.ShikiDaemonListFilesProcedure, rpc.ShikiDaemonStatFileProcedure, rpc.ShikiDaemonReadFileProcedure, rpc.ShikiDaemonWriteFileProcedure, rpc.ShikiDaemonDeleteFileProcedure:
		return true
	}
	return false
}

type countACKWriter struct {
	http.ResponseWriter
	n *atomic.Int64
}

func (w *countACKWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.n.Add(int64(n))
	return n, err
}
func (w *countACKWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Decode the actual successful Connect response, including its terminal
// envelope. A status code, ordinary RPC error or destination hash alone does
// not demonstrate that the server produced a completed commit acknowledgement.
func decodeCommit(r *http.Response, data []byte) (*pb.FileInfo, error) {
	if r.StatusCode != http.StatusOK || len(data) > 1<<20 {
		return nil, fmt.Errorf("commit response status=%d bytes=%d", r.StatusCode, len(data))
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/connect+proto") {
		return nil, fmt.Errorf("unexpected commit response type %q", r.Header.Get("Content-Type"))
	}
	var info *pb.FileInfo
	terminal := false
	for len(data) > 0 {
		if len(data) < 5 {
			return nil, errors.New("truncated Connect envelope")
		}
		flags, size := data[0], int(binary.BigEndian.Uint32(data[1:5]))
		data = data[5:]
		if size > len(data) {
			return nil, errors.New("truncated Connect payload")
		}
		payload := data[:size]
		data = data[size:]
		if flags&1 != 0 {
			if r.Header.Get("Connect-Content-Encoding") != "gzip" {
				return nil, errors.New("unsupported fixture response compression")
			}
			gz, err := gzip.NewReader(bytes.NewReader(payload))
			if err != nil {
				return nil, err
			}
			payload, err = io.ReadAll(io.LimitReader(gz, (1<<20)+1))
			gz.Close()
			if err != nil || len(payload) > 1<<20 {
				return nil, errors.New("invalid compressed commit envelope")
			}
		}
		if flags&2 != 0 {
			var end struct {
				Error json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(payload, &end); err != nil {
				return nil, err
			}
			if len(end.Error) > 0 && string(end.Error) != "null" {
				return nil, errors.New("upstream commit ended with an RPC error")
			}
			if terminal || len(data) != 0 {
				return nil, errors.New("invalid repeated or nonfinal terminal envelope")
			}
			terminal = true
		} else {
			if info != nil || terminal {
				return nil, errors.New("invalid repeated commit metadata")
			}
			info = new(pb.FileInfo)
			if err := proto.Unmarshal(payload, info); err != nil {
				return nil, err
			}
		}
	}
	if info == nil || !terminal || info.Revision == "" || info.ContentHash == "" {
		return nil, errors.New("successful commit response incomplete")
	}
	return info, nil
}
