package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/net/http2"
	"tailscale.com/control/controlhttp/controlhttpserver"
	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// rejectControl serves real Noise registration requests. A key-bearing request
// cannot receive its HTTP rejection until the test opens the response barrier.
// Anonymous startup may still report NeedsLogin before that response exists.
type rejectControl struct {
	url      string
	request  chan struct{}
	release  chan struct{}
	rejected atomic.Int64
}

func newRejectControl(t *testing.T, ctx context.Context) *rejectControl {
	t.Helper()
	netns.SetEnabled(false)
	t.Cleanup(func() { netns.SetEnabled(true) })
	fixture := &rejectControl{request: make(chan struct{}), release: make(chan struct{})}
	private := key.NewMachine()
	var requested sync.Once
	var handlers sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	register := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/machine/register" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var request tailcfg.RegisterRequest
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.Auth == nil || request.Auth.AuthKey == "" {
			if request.Followup != "" {
				<-r.Context().Done()
				return
			}
			_ = json.NewEncoder(w).Encode(tailcfg.RegisterResponse{AuthURL: fixture.url + "/auth/pending"})
			return
		}
		requested.Do(func() { close(fixture.request) })
		select {
		case <-r.Context().Done():
			return
		case <-fixture.release:
		}
		fixture.rejected.Add(1)
		http.Error(w, "invalid key: test rejection", http.StatusBadRequest)
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/key":
			_ = json.NewEncoder(w).Encode(tailcfg.OverTLSPublicKeyResponse{PublicKey: private.Public()})
		case "/ts2021":
			handlers.Add(1)
			defer handlers.Done()
			conn, err := controlhttpserver.AcceptHTTP(ctx, w, r, private, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			stop := context.AfterFunc(ctx, func() { conn.Close() })
			defer stop()
			var h2 http2.Server
			h2.ServeConn(conn, &http2.ServeConnOpts{Context: ctx, BaseConfig: &http.Server{Handler: register}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	fixture.url = server.URL
	t.Cleanup(func() {
		cancel()
		server.Close()
		handlers.Wait() // httptest alone cannot wait for hijacked Noise handlers.
	})
	return fixture
}

type subscribedTransport struct {
	transport  *http.Transport
	subscribed chan struct{}
	once       sync.Once
}

func (s *subscribedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := s.transport.RoundTrip(request)
	if err == nil && response.StatusCode == http.StatusOK && request.URL.Path == "/localapi/v0/watch-ipn-bus" {
		// LocalAPI flushes these headers only after installing its bus watcher.
		s.once.Do(func() { close(s.subscribed) })
	}
	return response, err
}

func awaitAuthBarrier(t *testing.T, ctx context.Context, barrier <-chan struct{}, phase string) {
	t.Helper()
	select {
	case <-barrier:
	case <-ctx.Done():
		t.Fatal("auth rejection fixture barrier: " + phase)
	}
}
