package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPluginMetadataAndArchiveFetchesAreBoundedAndCancelable(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oversized" {
			_, _ = w.Write(bytes.Repeat([]byte("x"), 65))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	original := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = original })
	if _, err := fetchPluginBytes(t.Context(), server.URL+"/oversized", 64); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("signature/index byte budget was not enforced: %v", err)
	}
	for _, archive := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		var err error
		if archive {
			var cleanup func()
			_, cleanup, err = fetchPluginSource(ctx, server.URL+"/blocked", "fixture.tar.gz")
			if cleanup != nil {
				cleanup()
			}
		} else {
			_, err = fetchPluginBytes(ctx, server.URL+"/blocked", 64)
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("fetch archive=%v ignored cancellation: %v", archive, err)
		}
	}
}
