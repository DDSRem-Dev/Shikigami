package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

// Client-side cancellation is insufficient when a host is blocked sending to
// an HTTP/2 peer that stopped reading. Retiring the worker must wake that send.
func TestWorkerRetirementInterruptsBackpressuredHTTP2Send(t *testing.T) {
	lifetime, revoke := context.WithCancel(t.Context())
	defer revoke()
	worker := &Instance{grant: &grant{lifetime: lifetime}, snapshot: Snapshot{State: "ready", Side: "server", Protocol: &pb.ProtocolAgreement{Capabilities: []string{sdk.FileCapability}}}}
	finished := make(chan error, 1)
	var attempted atomic.Int64
	server := httptest.NewUnstartedServer(WithStreamCancellation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, release, err := worker.workerCall(r.Context(), sdk.FileCapability)
		if err != nil {
			finished <- err
			return
		}
		defer release()
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
		data := make([]byte, 256<<10)
		for range 4096 {
			attempted.Add(int64(len(data)))
			if _, err := w.Write(data); err != nil {
				finished <- err
				return
			}
		}
		finished <- nil
	})))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.ProtoMajor != 2 {
		t.Fatal("test requires HTTP/2")
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for attempted.Load() < 4<<20 {
		select {
		case err := <-finished:
			t.Fatalf("sender did not backpressure: %v", err)
		case <-deadline.C:
			t.Fatal("sender failed to fill HTTP/2 window")
		case <-ticker.C:
		}
	}
	revoke()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("retirement did not interrupt the response")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker retirement left HTTP/2 sender blocked")
	}
}
