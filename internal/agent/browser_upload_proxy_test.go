package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
)

type terminalBrowserUpload struct {
	erpc.UnimplementedBrowserServiceHandler
	final error
}

func (h *terminalBrowserUpload) WriteBrowserFile(_ context.Context, stream erpc.BrowserServiceWriteBrowserFileServerStream) (*epb.BrowserFile, error) {
	hash := sha256.New()
	var size int64
	for {
		chunk, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		size += int64(len(chunk.Data))
		_, _ = hash.Write(chunk.Data)
	}
	if h.final != nil {
		return nil, h.final
	}
	return &epb.BrowserFile{Name: "receipt.bin", Size: size, Sha256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func TestBrowserUploadProxyPreservesTerminalResult(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "committed-receipt"
		if failure {
			name = "terminal-error"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			pool, lifetime := newCountedPool(t)
			pool.setCapabilities(&pb.ProtocolAgreement{Capabilities: []string{sdk.BrowserCapability}})
			open := func(handler http.Handler) *connect.Client {
				t.Helper()
				server := httptest.NewUnstartedServer(handler)
				server.EnableHTTP2 = true
				server.StartTLS()
				t.Cleanup(server.Close)
				roots := x509.NewCertPool()
				roots.AddCert(server.Certificate())
				endpoint, err := connection.NewTLSClient(server.URL, connection.TLSOptions{RootCAs: roots})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(endpoint.Close)
				return connection.NewRPCClient(endpoint)
			}
			remote := connect.NewServer()
			handler := &terminalBrowserUpload{}
			if failure {
				handler.final = connect.NewError(connect.CodeDataLoss, "UPLOAD_COMMIT_FAILED")
			}
			erpc.RegisterBrowserServiceHandler(remote, handler)
			base := open(connection.NewRPCHandler(remote))
			pooled := connect.NewClient(clientTransport{client: base}, pool.bind)
			type bodyKey struct{}
			proxy := connect.NewServer()
			host.RegisterExtensionProxy(proxy, func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
				return host.ProxyExtension(ctx, spec, stream, pooled, func() { _ = ctx.Value(bodyKey{}).(io.Closer).Close() })
			})
			proxyHandler := connection.NewRPCHandler(proxy)
			client := erpc.NewBrowserServiceClient(open(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyHandler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), bodyKey{}, r.Body)))
			})))
			payload := bytes.Repeat([]byte("browser staged upload\n"), 4096)
			stream, err := client.WriteBrowserFile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, data := range [][]byte{payload[:len(payload)/2], payload[len(payload)/2:]} {
				if err := stream.Send(&epb.BrowserFileChunk{ProfileId: "profile", Name: "receipt.bin", Data: data}); err != nil {
					t.Fatal(err)
				}
			}
			receipt, err := stream.CloseAndReceive()
			if failure {
				if connect.CodeOf(err) != connect.CodeDataLoss {
					t.Fatalf("terminal error became success or cancellation: %v", err)
				}
			} else {
				sum := sha256.Sum256(payload)
				if err != nil || receipt.GetName() != "receipt.bin" || receipt.GetSize() != int64(len(payload)) || receipt.GetSha256() != hex.EncodeToString(sum[:]) {
					t.Fatalf("committed upload lost verified receipt: %v, %v", receipt, err)
				}
			}
			lifetime.awaitCount(t, 0)
			if pool.bad.Load() || lifetime.Err() != nil {
				t.Fatal("upload completion invalidated shared remote pool")
			}
		})
	}
}
