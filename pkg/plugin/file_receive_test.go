package plugin

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type cancelFileWorker struct {
	FileWorkerService
	ctx           context.Context
	err           chan error
	ignoreFailure bool
}

func (w *cancelFileWorker) WriteFile(_ context.Context, receive func(context.Context) (*pb.WriteFileRequest, error)) (*pb.FileInfo, error) {
	_, err := receive(w.ctx)
	// A worker cannot resume reads after cancellation, even with a fresh ctx.
	_, again := receive(context.Background())
	if !errors.Is(again, err) {
		w.err <- errors.New("canceled reader accepted a new receive")
	} else {
		w.err <- nil
	}
	if w.ignoreFailure {
		return &pb.FileInfo{Path: "must-not-succeed"}, nil
	}
	return nil, err
}

type observedFileStream struct {
	grpc.ServerStream
	entered, exited chan struct{}
	calls           *atomic.Int32
}

func (s *observedFileStream) RecvMsg(message any) error {
	if s.calls.Add(1) == 1 {
		close(s.entered)
		defer close(s.exited)
	}
	return s.ServerStream.RecvMsg(message)
}

func TestFileReceiverGRPCHandlerCancellationReleasesTransport(t *testing.T) {
	for _, test := range []struct {
		name                    string
		deadline, ignoreFailure bool
	}{
		{name: "cancel"},
		{name: "deadline", deadline: true},
		{name: "ignored cancel", ignoreFailure: true},
		{name: "ignored deadline", deadline: true, ignoreFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			entered, exited := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())
			if test.deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 300*time.Millisecond)
			}
			defer cancel()
			worker := &cancelFileWorker{ctx: ctx, err: make(chan error, 1), ignoreFailure: test.ignoreFailure}
			listener := bufconn.Listen(64 << 10)
			server := grpc.NewServer(grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
				return handler(srv, &observedFileStream{ServerStream: stream, entered: entered, exited: exited, calls: &calls})
			}))
			ppb.RegisterFileServiceServer(server, &fileServer{impl: worker})
			serveDone := make(chan struct{})
			go func() { defer close(serveDone); _ = server.Serve(listener) }()
			t.Cleanup(func() { server.Stop(); listener.Close(); <-serveDone })
			conn, err := grpc.NewClient("passthrough:///file-receive", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			rpcCtx, cancelRPC := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelRPC()
			stream, err := ppb.NewFileServiceClient(conn).WriteFile(rpcCtx)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-rpcCtx.Done():
				t.Fatal("transport receive never started")
			}
			if !test.deadline {
				cancel()
			}
			// RecvMsg reads only the response: do not CloseSend or cancel the
			// client, which would hide failure to release the server reader.
			err = stream.RecvMsg(&pb.FileInfo{})
			want := codes.Canceled
			if test.deadline {
				want = codes.DeadlineExceeded
			}
			if status.Code(err) != want {
				t.Fatalf("RPC error = %v; want %v", err, want)
			}
			select {
			case <-exited:
			case <-rpcCtx.Done():
				t.Fatal("handler returned but real transport RecvMsg did not exit")
			}
			if err := <-worker.err; err != nil {
				t.Fatal(err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("transport reads = %d; want one", got)
			}
		})
	}
}
