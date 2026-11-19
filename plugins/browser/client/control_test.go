package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

type controlBodyReader struct {
	io.Reader
	beforeRead func()
}

func (r *controlBodyReader) Read(p []byte) (int, error) {
	if r.beforeRead != nil {
		f := r.beforeRead
		r.beforeRead = nil
		f()
	}
	return r.Reader.Read(p)
}

func TestViewerControlDoesNotRebindDelayedRequest(t *testing.T) {
	for _, scenario := range []struct {
		name, body string
		status     int
	}{
		{"unscoped legacy request", `{"revision":"same-revision"}`, http.StatusBadRequest},
		{"old attachment", `{"revision":"same-revision","attachment":"old-attachment","instance":"instance"}`, http.StatusConflict},
		{"wrong instance", `{"revision":"same-revision","attachment":"new-attachment","instance":"other-instance"}`, http.StatusConflict},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			old := &attachment{ctx: ctx, id: "old-attachment", instance: "instance", canControl: true}
			v := &viewer{ctx: t.Context(), serverID: "server", profileID: "profile", connection: old}
			var forwarded *epb.BrowserControlRequest
			w := &Worker{config: viewerConfig{operationTimeout: time.Second}, remote: &viewerTestBrowser{control: func(_ context.Context, r *epb.BrowserControlRequest) (*epb.BrowserStatus, error) {
				forwarded = r
				return &epb.BrowserStatus{}, nil
			}}}
			request := httptest.NewRequest(http.MethodPost, "http://localhost/api/control", &controlBodyReader{Reader: strings.NewReader(scenario.body), beforeRead: func() {
				v.mu.Lock()
				cancel()
				v.connection = &attachment{ctx: t.Context(), id: "new-attachment", instance: "instance", canControl: true}
				v.mu.Unlock()
			}})
			request.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			w.handleControl(out, request, v)
			if forwarded != nil || out.Code != scenario.status {
				t.Fatalf("delayed authorization was not rejected for its original attachment: status=%d forwarded=%v", out.Code, forwarded)
			}
		})
	}
}

func TestViewerControlEndsWithItsAttachment(t *testing.T) {
	attachmentCtx, endAttachment := context.WithCancel(t.Context())
	defer endAttachment()
	v := &viewer{ctx: t.Context(), serverID: "server", profileID: "profile", connection: &attachment{ctx: attachmentCtx, id: "attachment", instance: "instance", canControl: true}}
	entered := make(chan struct{})
	finished := make(chan struct{})
	w := &Worker{config: viewerConfig{operationTimeout: 10 * time.Second}, remote: &viewerTestBrowser{control: func(ctx context.Context, _ *epb.BrowserControlRequest) (*epb.BrowserStatus, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/control", strings.NewReader(`{"revision":"revision","attachment":"attachment","instance":"instance"}`))
	request.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	go func() { w.handleControl(out, request, v); close(finished) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("control never reached the remote")
	}
	endAttachment()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("detached attachment left its control request alive")
	}
	if out.Code != http.StatusRequestTimeout {
		t.Fatalf("canceled control status=%d", out.Code)
	}
}
