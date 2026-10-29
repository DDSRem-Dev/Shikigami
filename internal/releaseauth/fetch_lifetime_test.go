package releaseauth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFetchCancellationAndDeadlineReleaseBlockedRequest(t *testing.T) {
	for _, stage := range []string{"signature-headers", "signature-body", "archive-headers", "archive-body"} {
		for _, interruption := range []string{"cancel", "deadline"} {
			t.Run(stage+"/"+interruption, func(t *testing.T) {
				started, stopped := make(chan struct{}), make(chan struct{})
				_, client, opts := fetchFixture(t, func(f *fixture, w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/signature" && strings.HasPrefix(stage, "archive") {
						_, _ = w.Write(readFile(t, f.sign.Signature))
						return
					}
					if strings.HasSuffix(stage, "body") {
						_, _ = w.Write([]byte("partial"))
						w.(http.Flusher).Flush()
					}
					close(started)
					<-r.Context().Done()
					close(stopped)
				})
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				defer cancel()
				finished := make(chan error, 1)
				go func() {
					result, err := fetch(ctx, opts, client)
					if result != (Verified{}) {
						finished <- errors.New("interrupted fetch returned receipt")
						return
					}
					finished <- err
				}()
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("request did not reach blocked stage")
				}
				want := context.DeadlineExceeded
				if interruption == "cancel" {
					want = context.Canceled
					cancel()
				}
				select {
				case err := <-finished:
					if !errors.Is(err, want) || strings.Contains(err.Error(), "secret-query") {
						t.Fatal("interruption identity lost or URL leaked", err)
					}
				case <-time.After(time.Second):
					t.Fatal("fetch did not return after cancellation")
				}
				select {
				case <-stopped:
				case <-time.After(time.Second):
					t.Fatal("blocked server request retained after cancellation")
				}
				absent(t, opts.Output)
			})
		}
	}
}

func TestFetchResponseHeaderTimeoutCancelsRequest(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	_, client, opts := fetchFixture(t, func(_ *fixture, _ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	})
	// Shorten only the private test client's transport timeout. The public
	// constructor keeps its fixed 10-second value and cannot accept a client.
	transport := client.Transport.(*http.Transport)
	transport.ResponseHeaderTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := fetch(ctx, opts, client)
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatal("transport header bound not enforced independently of parent", err)
	}
	select {
	case <-started:
	default:
		t.Fatal("timeout did not reach real HTTPS request")
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("header timeout did not cancel server request")
	}
	absent(t, opts.Output)
}

func TestFetchDefaultSignatureDeadlineStopsSilentBody(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	_, client, opts := fetchFixture(t, func(_ *fixture, w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(stopped)
	})
	ctx, cancel := context.WithTimeout(context.Background(), signatureTimeout+5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := fetch(ctx, opts, client)
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || time.Since(start) < signatureTimeout-100*time.Millisecond {
		t.Fatal("default signature body deadline not enforced", err, time.Since(start))
	}
	select {
	case <-started:
	default:
		t.Fatal("silent body request never reached server")
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("signature deadline left server request active")
	}
	absent(t, opts.Output)
}
