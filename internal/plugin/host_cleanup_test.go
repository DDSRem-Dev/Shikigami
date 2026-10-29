package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
)

// Models the pinned go-plugin contract: Kill with no runner does not prevent
// a future Client call from starting one. Integration tests exercise that
// contract with a real SDK subprocess; this gate fixes the exact interleaving.
type lateStartProcess struct {
	mu        sync.Mutex
	firstKill chan struct{}
	start     chan struct{}
	running   bool
	kills     int
}

func (p *lateStartProcess) Client() (goplugin.ClientProtocol, error) {
	<-p.start
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()
	return nil, nil
}
func (p *lateStartProcess) ReattachConfig() *goplugin.ReattachConfig { return nil }
func (p *lateStartProcess) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kills++
	p.running = false
	if p.kills == 1 {
		close(p.firstKill)
	}
}

func TestInstanceCleanupJoinsLateClientStart(t *testing.T) {
	lifetime, cancel := context.WithCancel(t.Context())
	process := &lateStartProcess{firstKill: make(chan struct{}), start: make(chan struct{})}
	instance := &Instance{process: process, clientDone: make(chan struct{}), done: make(chan struct{}), dir: t.TempDir(), grant: &grant{lifetime: lifetime, cancel: cancel}}
	go func() { _, _ = process.Client(); close(instance.clientDone) }()
	ctx, stop := context.WithCancel(context.Background())
	stop()
	_ = instance.Stop(ctx)
	select {
	case <-process.firstKill:
	case <-time.After(time.Second):
		close(process.start)
		t.Fatal("cleanup did not attempt its initial Kill")
	}
	select {
	case <-instance.Done():
		t.Error("cleanup completed before pending Client start joined")
	default:
	}
	close(process.start)
	select {
	case <-instance.Done():
	case <-time.After(time.Second):
		t.Fatal("cleanup did not join the released Client start")
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.running {
		t.Fatal("instance reported stopped while late-started child remained running")
	}
}

func TestStoppingCoreHandlerRefusesAdmission(t *testing.T) {
	instance := new(Instance)
	instance.closeAdmission()
	called := false
	handler := instance.coreHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/shiki.plugin.CoreAPI/GetConfig", nil))
	if called || response.Code != http.StatusServiceUnavailable || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("stopping request admitted or cacheable: called=%v code=%d headers=%v", called, response.Code, response.Header())
	}
}
