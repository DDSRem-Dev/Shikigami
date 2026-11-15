package plugin

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cancelBrowserUpload struct {
	BrowserService
	ctx           context.Context
	err           chan error
	ignoreFailure bool
}

func (p *cancelBrowserUpload) WriteBrowserFile(_ context.Context, receive func(context.Context) (*epb.BrowserFileChunk, error)) (*epb.BrowserFile, error) {
	_, err := receive(p.ctx)
	_, again := receive(context.Background())
	if !errors.Is(again, err) {
		p.err <- errors.New("canceled browser receiver accepted a new receive")
	} else {
		p.err <- nil
	}
	if p.ignoreFailure {
		return &epb.BrowserFile{Name: "must-not-succeed"}, nil
	}
	return nil, err
}

func TestBrowserUploadCancellationIsTerminalAndReleasesTransport(t *testing.T) {
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
			ctx, cancel := context.WithCancel(t.Context())
			if test.deadline {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 300*time.Millisecond)
			}
			defer cancel()
			probe := &cancelBrowserUpload{ctx: ctx, err: make(chan error, 1), ignoreFailure: test.ignoreFailure}
			client, _ := nativeExtensionClient(t, Services{Browser: probe}, grpc.ChainStreamInterceptor(func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
				return next(srv, &observedFileStream{ServerStream: stream, entered: entered, exited: exited, calls: &calls})
			}))
			rpcCtx, cancelRPC := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancelRPC()
			stream, err := client.Browser().WriteBrowserFile(rpcCtx)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-rpcCtx.Done():
				t.Fatal("browser transport receive never started")
			}
			if !test.deadline {
				cancel()
			}
			// Do not CloseSend or cancel the RPC: the SDK must release its
			// blocked reader when the implementation's receive is canceled.
			_, err = stream.Receive()
			want := codes.Canceled
			if test.deadline {
				want = codes.DeadlineExceeded
			}
			if got := ToGRPC(err); status.Code(got) != want {
				t.Fatalf("upload error = %v; want %v", err, want)
			}
			select {
			case <-exited:
			case <-rpcCtx.Done():
				t.Fatal("browser transport reader survived handler return")
			}
			if err := <-probe.err; err != nil {
				t.Fatal(err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("browser transport reads = %d; want one", got)
			}
		})
	}
}
