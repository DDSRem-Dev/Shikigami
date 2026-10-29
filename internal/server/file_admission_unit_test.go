package server

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type admissionStreamStub struct{ receive func(any) error }

func (s admissionStreamStub) Receive(v any) error { return s.receive(v) }
func (admissionStreamStub) Send(any) error        { return nil }
func (admissionStreamStub) SendHeaders() error    { return nil }

func TestFileAdmissionReleasesEveryExit(t *testing.T) {
	for _, outcome := range []string{"success", "error", "cancel", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			var gate fileAdmission
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			control := &requestReadControl{readDeadline: func(time.Time) error { return nil }, closeRead: func() error { return nil }}
			ctx = context.WithValue(ctx, requestReadKey{}, control)
			sentinel := errors.New("handler failure")
			next := gate.interceptor()(func(ctx context.Context, _ connect.Spec, _ connect.ServerStream) error {
				if gate.transfers.Load() != 1 {
					t.Fatal("handler did not hold one slot")
				}
				switch outcome {
				case "error":
					return sentinel
				case "cancel":
					cancel()
					return ctx.Err()
				case "panic":
					panic(sentinel)
				}
				return nil
			})
			var got error
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				got = next(ctx, connect.Spec{Procedure: rpc.ShikiDaemonReadFileProcedure}, admissionStreamStub{})
			}()
			if gate.transfers.Load() != 0 {
				t.Fatal("handler exit retained admission slot")
			}
			switch outcome {
			case "success":
				if got != nil || recovered != nil {
					t.Fatalf("success: %v, panic: %v", got, recovered)
				}
			case "error":
				if !errors.Is(got, sentinel) {
					t.Fatal("handler error lost", got)
				}
			case "cancel":
				if !errors.Is(got, context.Canceled) {
					t.Fatal("handler cancellation lost", got)
				}
			case "panic":
				if recovered != sentinel {
					t.Fatal("handler panic was swallowed", recovered)
				}
			}
		})
	}
}

func TestFileAdmissionCanceledBeforeSaturatedBudget(t *testing.T) {
	var gate fileAdmission
	gate.transfers.Store(limits.FileTransfers)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	next := gate.interceptor()(func(context.Context, connect.Spec, connect.ServerStream) error {
		t.Fatal("canceled request reached handler")
		return nil
	})
	if err := next(ctx, connect.Spec{Procedure: rpc.ShikiDaemonReadFileProcedure}, admissionStreamStub{}); connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatal("cancellation lost to saturated budget", err)
	}
	if gate.transfers.Load() != limits.FileTransfers {
		t.Fatal("canceled request changed admission count")
	}
}

func TestFileAdmissionFirstSuccessClearsReadDeadline(t *testing.T) {
	for _, earlier := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "earlier-parent"}[earlier], func(t *testing.T) {
			ctx := t.Context()
			parentDeadline := time.Now().Add(time.Second)
			if earlier {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, parentDeadline)
				defer cancel()
			}
			var deadlines []time.Time
			control := &requestReadControl{readDeadline: func(d time.Time) error { deadlines = append(deadlines, d); return nil }, closeRead: func() error { return nil }}
			ctx = context.WithValue(ctx, requestReadKey{}, control)
			var gate fileAdmission
			started := time.Now()
			next := gate.interceptor()(func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
				if err := stream.Receive(&pb.WatchFilesRequest{}); err != nil {
					return err
				}
				return stream.Receive(&pb.WatchFilesRequest{})
			})
			calls := 0
			err := next(ctx, connect.Spec{Procedure: rpc.ShikiDaemonWatchFilesProcedure}, admissionStreamStub{receive: func(any) error { calls++; return nil }})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || len(deadlines) != 2 || !deadlines[1].IsZero() {
				t.Fatalf("first successful Receive must clear once without rearming: calls=%d deadlines=%v", calls, deadlines)
			}
			if earlier {
				if !deadlines[0].Equal(parentDeadline) {
					t.Fatal("earlier parent deadline extended", deadlines[0])
				}
			} else if deadlines[0].Before(started.Add(limits.FileFirstFrameTimeout)) || deadlines[0].After(time.Now().Add(limits.FileFirstFrameTimeout)) {
				t.Fatal("wrong first-frame deadline", deadlines[0])
			}
		})
	}
}

type admissionWatchProbe struct {
	rpc.UnimplementedShikiDaemonHandler
	release <-chan struct{}
}

func (p admissionWatchProbe) WatchFiles(ctx context.Context, _ *pb.WatchFilesRequest, stream rpc.ShikiDaemonWatchFilesServerStream) error {
	if err := stream.Send(&pb.FileEvent{}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.release:
		return stream.Send(&pb.FileEvent{})
	}
}

func TestFileAdmissionWatchOutlivesFirstFrameDeadline(t *testing.T) {
	manager, err := auth.Open(filepath.Join(t.TempDir(), "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	service := &Service{Auth: manager}
	release := make(chan struct{})
	server := connect.NewServer(manager.Interceptor(), service.fileCalls.interceptor())
	rpc.RegisterShikiDaemonHandler(server, admissionWatchProbe{release: release})
	remote := httptest.NewUnstartedServer(connection.NewRPCHandler(server, manager.HTTPMiddleware, firstRequestTransport))
	remote.EnableHTTP2 = true
	remote.StartTLS()
	t.Cleanup(remote.Close)
	fixture := &admissionFixture{auth: manager, service: service, remote: remote}
	client := fixture.client(t, "*")
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	stream, err := rpc.NewShikiDaemonClient(client).WatchFiles(ctx, &pb.WatchFilesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err = stream.Receive(); err != nil {
		t.Fatal("first event", err)
	}
	select {
	case <-time.After(limits.FileFirstFrameTimeout + 150*time.Millisecond):
	case <-ctx.Done():
		t.Fatal("Watch ended before first-frame interval", ctx.Err())
	}
	if service.fileCalls.watches.Load() != 1 {
		t.Fatal("live Watch lost admission slot after five seconds")
	}
	close(release)
	if _, err = stream.Receive(); err != nil {
		t.Fatal("Watch failed after five seconds", err)
	}
	if _, err = stream.Receive(); !errors.Is(err, io.EOF) {
		t.Fatal("Watch did not finish cleanly", err)
	}
	waitFileCount(t, &service.fileCalls.watches, 0)
}
