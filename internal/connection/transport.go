// Package connection owns trusted HTTP/2 transports. Constructing a client does
// not establish connectivity: callers must authenticate and negotiate first.
package connection

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
)

const MaxMessageBytes = limits.MaxMessageBytes
const MaxDataBytes = limits.MaxDataBytes

var ErrHTTP2Required = errors.New("HTTP2_REQUIRED")

// Endpoint binds a connection pool to exactly one trusted authority. Do not
// reuse an Endpoint for multiple credential domains.
type Endpoint struct {
	HTTPClient *http.Client
	BaseURL    string
	transport  *http.Transport
}

func (e *Endpoint) Close() { e.transport.CloseIdleConnections() }

func NewRPCClient(e *Endpoint, interceptors ...connect.ClientInterceptor) *connect.Client {
	interceptors = append(append([]connect.ClientInterceptor(nil), interceptors...), closeOnCancellation)
	return connect.NewClient(connecthttp.NewTransport(e.HTTPClient, e.BaseURL,
		connecthttp.WithCompressors(), connecthttp.WithReadMaxBytes(MaxMessageBytes),
		connecthttp.WithSendMaxBytes(MaxMessageBytes)), interceptors...)
}

type TLSOptions struct {
	ServerName string
	RootCAs    *x509.CertPool
}

// NewTLSClient accepts an HTTPS origin only. Environment proxies, redirects,
// HTTP/1, insecure certificate validation, and alternate targets are disabled.
func NewTLSClient(origin string, opts TLSOptions) (*Endpoint, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("TLS endpoint must be an HTTPS origin")
	}
	p := new(http.Protocols)
	p.SetHTTP2(true)
	name := opts.ServerName
	if name == "" {
		name = u.Hostname()
	}
	transport := &http.Transport{
		Protocols: p, Proxy: nil,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second, IdleConnTimeout: 90 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: opts.RootCAs, ServerName: name, NextProtos: []string{"h2"}, VerifyConnection: func(cs tls.ConnectionState) error {
			if cs.NegotiatedProtocol != "h2" {
				return ErrHTTP2Required
			}
			return nil
		}},
	}
	return newEndpoint("https://"+u.Host, transport), nil
}

func newEndpoint(origin string, transport *http.Transport) *Endpoint {
	u, _ := url.Parse(origin)
	return &Endpoint{BaseURL: origin, transport: transport, HTTPClient: &http.Client{
		Transport:     fixedOrigin{next: transport, scheme: u.Scheme, host: u.Host},
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("RPC redirects are disabled") },
	}}
}

type fixedOrigin struct {
	next         http.RoundTripper
	scheme, host string
}

func (f fixedOrigin) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != f.scheme || r.URL.Host != f.host || r.URL.User != nil || (r.Host != "" && r.Host != f.host) {
		return nil, errors.New("RPC target differs from trusted endpoint")
	}
	return f.next.RoundTrip(r)
}

// BearerToken belongs on a client created from a trusted remote Endpoint, never
// on the local IPC or plugin clients. It replaces caller-provided authorization.
func BearerToken(token string) (connect.ClientInterceptor, error) {
	if !strings.HasPrefix(token, "shiki_") || strings.ContainsAny(token, "\r\n \t") {
		return nil, errors.New("invalid token format")
	}
	return func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			info, ok := connect.CallInfoForClientContext(ctx)
			if !ok {
				ctx, info = connect.NewClientContext(ctx)
			}
			info.RequestHeader().Set("Authorization", "Bearer "+token)
			return next(ctx, spec)
		}
	}, nil
}

func NewHTTPServer(ctx context.Context, handler http.Handler) *http.Server {
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	p.SetHTTP2(true)
	return &http.Server{Handler: handler, Protocols: p, ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes: 32 << 10, IdleTimeout: 90 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if verified, ok := c.(*uidConn); ok {
				return context.WithValue(ctx, peerUIDKey{}, verified.uid)
			}
			return ctx
		},
	}
}

// ServeTLS requires an explicit certificate; the listener may be publicly
// reachable. Serve plain HTTP only on a verified Unix or tsnet listener.
func ServeTLS(server *http.Server, listener net.Listener, certFile, keyFile string) error {
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("TLS certificate and key are required")
	}
	certificate, err := LoadTLSCertificate(certFile, keyFile)
	if err != nil {
		return err
	}
	server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"h2"}, Certificates: []tls.Certificate{certificate}}
	return server.ServeTLS(listener, "", "")
}
