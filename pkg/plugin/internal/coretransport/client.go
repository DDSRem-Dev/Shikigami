// Package coretransport is the SDK-private CoreAPI client transport. It only
// dials the explicitly provided same-user Unix socket; it has no remote TLS,
// tailnet, credential-store, server or host business dependency.
package coretransport

import (
	"errors"
	"net/http"
	"net/url"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
)

type Endpoint struct {
	HTTPClient *http.Client
	BaseURL    string
	transport  *http.Transport
}

func (e *Endpoint) Close() { e.transport.CloseIdleConnections() }
func NewRPCClient(e *Endpoint, interceptors ...connect.ClientInterceptor) *connect.Client {
	interceptors = append(append([]connect.ClientInterceptor(nil), interceptors...), unaryDeadline, closeOnCancellation)
	return connect.NewClient(connecthttp.NewTransport(e.HTTPClient, e.BaseURL, connecthttp.WithCompressors(), connecthttp.WithReadMaxBytes(limits.MaxMessageBytes), connecthttp.WithSendMaxBytes(limits.MaxMessageBytes)), interceptors...)
}
func newEndpoint(origin string, transport *http.Transport) *Endpoint {
	target, _ := url.Parse(origin)
	return &Endpoint{BaseURL: origin, transport: transport, HTTPClient: &http.Client{Transport: fixedOrigin{next: transport, host: target.Host}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("CoreAPI redirects are disabled") }}}
}

type fixedOrigin struct {
	next http.RoundTripper
	host string
}

func (f fixedOrigin) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "http" || request.URL.Host != f.host || request.URL.User != nil || request.Host != "" && request.Host != f.host {
		return nil, errors.New("CoreAPI target differs from trusted endpoint")
	}
	return f.next.RoundTrip(request)
}
