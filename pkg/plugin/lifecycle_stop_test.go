package plugin

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type waitingStopWorker struct {
	Implementation
	release chan struct{}
	calls   atomic.Int32
}

func (w *waitingStopWorker) Stop(ctx context.Context) error {
	w.calls.Add(1)
	select {
	case <-w.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRuntimeStopRetriesIncompleteWorkerShutdown(t *testing.T) {
	worker := &waitingStopWorker{release: make(chan struct{})}
	r := &runtime{impl: worker, state: "running"}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, err := r.Stop(ctx, &pb.Empty{})
		cancel()
		if status.Code(err) != codes.DeadlineExceeded {
			t.Errorf("Stop %d = %v; want deadline exceeded until worker completes", i, err)
		}
	}
	close(worker.release)
	if _, err := r.Stop(context.Background(), &pb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Stop(context.Background(), &pb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if got := worker.calls.Load(); got != 3 {
		t.Errorf("worker Stop called %d times; want two incomplete and one successful shutdown", got)
	}
}
