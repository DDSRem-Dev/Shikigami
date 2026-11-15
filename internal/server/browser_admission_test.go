package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
)

func TestBrowserAdmissionUsesSharedFileBudgetsAndFirstFrameRules(t *testing.T) {
	var gate fileAdmission
	for _, test := range []struct {
		procedure string
		active    *atomic.Int32
		maximum   int32
	}{
		{erpc.BrowserServiceListBrowserFilesProcedure, &gate.metadata, limits.FileMetadataCalls},
		{erpc.BrowserServiceReadBrowserFileProcedure, &gate.transfers, limits.FileTransfers},
		{erpc.BrowserServiceWriteBrowserFileProcedure, &gate.transfers, limits.FileTransfers},
		{erpc.BrowserServiceBrowserStreamProcedure, &gate.watches, limits.FileWatches},
	} {
		t.Run(test.procedure, func(t *testing.T) {
			active, maximum := gate.budget(test.procedure)
			if active != test.active || maximum != test.maximum {
				t.Fatal("browser allocated an independent file budget")
			}
			active.Store(maximum)
			next := gate.interceptor()(func(context.Context, connect.Spec, connect.ServerStream) error {
				t.Fatal("saturated browser call reached the handler")
				return nil
			})
			if err := next(t.Context(), connect.Spec{Procedure: test.procedure}, admissionStreamStub{}); connect.CodeOf(err) != connect.CodeResourceExhausted {
				t.Fatal("browser bypassed saturated host file admission", err)
			}
			active.Store(0)
			for _, parent := range []bool{false, true} {
				ctx := t.Context()
				deadline := time.Now().Add(time.Second)
				if parent {
					var cancel context.CancelFunc
					ctx, cancel = context.WithDeadline(ctx, deadline)
					defer cancel()
				}
				var deadlines []time.Time
				control := &requestReadControl{readDeadline: func(d time.Time) error { deadlines = append(deadlines, d); return nil }, closeRead: func() error { return nil }}
				ctx = context.WithValue(ctx, requestReadKey{}, control)
				started := time.Now()
				calls := 0
				next = gate.interceptor()(func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
					if active.Load() != 1 {
						t.Fatal("browser handler did not hold its host slot")
					}
					if err := stream.Receive(&epb.BrowserFileChunk{}); err != nil {
						return err
					}
					return stream.Receive(&epb.BrowserFileChunk{})
				})
				if err := next(ctx, connect.Spec{Procedure: test.procedure}, admissionStreamStub{receive: func(any) error { calls++; return nil }}); err != nil {
					t.Fatal(err)
				}
				if active.Load() != 0 || calls != 2 || len(deadlines) != 2 || !deadlines[1].IsZero() {
					t.Fatalf("browser retained admission or first-frame deadline: count=%d calls=%d deadlines=%v", active.Load(), calls, deadlines)
				}
				if parent {
					if !deadlines[0].Equal(deadline) {
						t.Fatal("browser extended parent deadline", deadlines[0])
					}
				} else if deadlines[0].Before(started.Add(limits.FileFirstFrameTimeout)) || deadlines[0].After(time.Now().Add(limits.FileFirstFrameTimeout)) {
					t.Fatal("browser did not inherit file first-frame default", deadlines[0])
				}
			}
		})
	}
}

func TestBrowserAdmissionConsumerSharesFileCapacityAfterAuthorization(t *testing.T) {
	f := newAdmissionFixture(t)
	f.service.fileCalls.metadata.Store(limits.FileMetadataCalls)
	f.service.fileCalls.transfers.Store(limits.FileTransfers)
	f.service.fileCalls.watches.Store(limits.FileWatches)
	client := erpc.NewBrowserServiceClient(f.client(t, "*"))
	if _, err := client.ListBrowserFiles(t.Context(), &epb.BrowserFileRequest{ProfileId: "profile"}); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal("browser metadata did not share file admission", err)
	}
	read, err := client.ReadBrowserFile(t.Context(), &epb.BrowserFileRequest{ProfileId: "profile"})
	if err == nil {
		_, err = read.Receive()
		_ = read.Close()
	}
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal("browser download did not share transfer admission", err)
	}
	write, err := client.WriteBrowserFile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_ = write.Send(&epb.BrowserFileChunk{ProfileId: "profile", Name: "file"})
	if _, err := write.CloseAndReceive(); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal("browser upload did not share transfer admission", err)
	}
	view, err := client.BrowserStream(t.Context())
	if err == nil {
		_, err = view.Receive()
		_ = view.Close()
	}
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal("browser attachment did not share long-stream admission", err)
	}
	denied := erpc.NewBrowserServiceClient(f.client(t, "project.list"))
	if _, err := denied.ListBrowserFiles(t.Context(), &epb.BrowserFileRequest{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("browser admission leaked before authorization", err)
	}
	f.service.fileCalls.metadata.Store(0)
	if _, err := client.ListBrowserFiles(t.Context(), &epb.BrowserFileRequest{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("released browser capacity did not reach plugin readiness admission", err)
	}
}

type browserFirstFrameProbe struct {
	erpc.UnimplementedBrowserServiceHandler
	done chan struct{}
}

func (p browserFirstFrameProbe) WriteBrowserFile(_ context.Context, stream erpc.BrowserServiceWriteBrowserFileServerStream) (*epb.BrowserFile, error) {
	defer close(p.done)
	_, err := stream.Receive()
	return nil, err
}
func (p browserFirstFrameProbe) BrowserStream(_ context.Context, stream erpc.BrowserServiceBrowserStreamServerStream) error {
	defer close(p.done)
	_, err := stream.Receive()
	return err
}

func TestBrowserHostFirstFrameDeadlineInterruptsSilentHTTP2Body(t *testing.T) {
	for _, procedure := range []string{erpc.BrowserServiceWriteBrowserFileProcedure, erpc.BrowserServiceBrowserStreamProcedure} {
		t.Run(procedure, func(t *testing.T) {
			f := newAdmissionFixture(t)
			done := make(chan struct{})
			server := connect.NewServer(f.auth.Interceptor(), f.service.fileCalls.interceptor())
			erpc.RegisterBrowserServiceHandler(server, browserFirstFrameProbe{done: done})
			remote := httptest.NewUnstartedServer(connection.NewRPCHandler(server, f.auth.HTTPMiddleware, firstRequestTransport, host.WithStreamCancellation))
			remote.EnableHTTP2 = true
			remote.StartTLS()
			defer remote.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, remote.URL+procedure, reader)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+f.heldToken)
			request.Header.Set("Content-Type", "application/connect+proto")
			request.Header.Set("Connect-Protocol-Version", "1")
			request.Header.Set("Connect-Timeout-Ms", "100")
			response, err := remote.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
			response.Body.Close()
			if err != nil || !bytes.Contains(body, []byte("deadline_exceeded")) {
				t.Fatalf("silent browser body did not receive a bounded deadline: %q %v", body, err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("first-frame deadline left browser Receive blocked")
			}
			active, _ := f.service.fileCalls.budget(procedure)
			if active.Load() != 0 {
				t.Fatal("silent browser deadline retained host admission")
			}
		})
	}
}
