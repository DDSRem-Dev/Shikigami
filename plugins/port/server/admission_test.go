package server

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/types/known/structpb"
)

type admissionScanKey struct{}
type admissionScanner struct{ started chan struct{} }

func (s *admissionScanner) Scan(ctx context.Context) ([]*pb.OpenPort, error) {
	if ctx.Value(admissionScanKey{}) != nil {
		s.started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []*pb.OpenPort{{BindAddress: "127.0.0.1", Port: 3000, Protocol: "tcp"}}, nil
}
func admissionWorker(t *testing.T, scanner Scanner) *Worker {
	t.Helper()
	w := NewWithScanner(scanner)
	config, err := structpb.NewStruct(map[string]any{"allow_ports": []any{float64(3000)}})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Init(context.Background(), sdk.InitContext{Side: "server", StateDir: t.TempDir(), Core: dummyCore{}, Config: config}); err != nil {
		t.Fatal(err)
	}
	if err = w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := w.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return w
}

type admittedWatch struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func openAdmissionWatch(t *testing.T, w *Worker) *admittedWatch {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	call := &admittedWatch{cancel: cancel, done: make(chan struct{})}
	ready := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() {
		cancel()
		select {
		case <-call.done:
		case <-time.After(time.Second):
			t.Error("watch did not stop")
		}
	})
	go func() {
		defer close(call.done)
		call.err = w.WatchPorts(ctx, &pb.WatchPortsRequest{}, func(event *pb.PortEvent) error {
			if event.GetType() != pb.PortEvent_OPENED {
				return errors.New("unexpected event")
			}
			once.Do(func() { close(ready) })
			return nil
		})
	}()
	select {
	case <-ready:
		return call
	case <-call.done:
		t.Fatalf("watch not admitted: %v", call.err)
	case <-time.After(time.Second):
		t.Fatal("watch initial scan did not finish")
	}
	return nil
}
func TestPortWatchBudgetDoesNotConsumeMetadata(t *testing.T) {
	w := admissionWorker(t, &admissionScanner{})
	var watches []*admittedWatch
	for i := 0; i < 32; i++ {
		watches = append(watches, openAdmissionWatch(t, w))
	}
	if _, err := w.ListPorts(context.Background(), &pb.ListPortsRequest{}); err != nil {
		t.Errorf("32 watches blocked ListPorts: %v", err)
	}
	if _, err := w.CheckPort(context.Background(), &pb.CheckPortRequest{RemoteAddress: "127.0.0.1", Port: 3000}); err != nil {
		t.Errorf("32 watches blocked CheckPort: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := w.WatchPorts(ctx, &pb.WatchPortsRequest{}, func(*pb.PortEvent) error { return errors.New("overflow watch admitted") })
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("33rd watch: %v", err)
	}
	watches[0].cancel()
	select {
	case <-watches[0].done:
	case <-ctx.Done():
		t.Fatal("watch slot not released")
	}
	openAdmissionWatch(t, w)
}
func TestPortMetadataBudgetDoesNotConsumeWatches(t *testing.T) {
	scanner := &admissionScanner{started: make(chan struct{}, 32)}
	w := admissionWorker(t, scanner)
	var cancelCalls []context.CancelFunc
	var finished []chan struct{}
	t.Cleanup(func() {
		for _, cancel := range cancelCalls {
			cancel()
		}
		for _, done := range finished {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("metadata call did not stop")
			}
		}
	})
	for i := 0; i < 32; i++ {
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), admissionScanKey{}, true))
		cancelCalls = append(cancelCalls, cancel)
		done := make(chan struct{})
		finished = append(finished, done)
		go func() { defer close(done); _, _ = w.ListPorts(ctx, &pb.ListPortsRequest{}) }()
	}
	for i := 0; i < 32; i++ {
		select {
		case <-scanner.started:
		case <-time.After(time.Second):
			t.Fatal("metadata calls not admitted")
		}
	}
	if _, err := w.ListPorts(context.Background(), &pb.ListPortsRequest{}); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("33rd metadata request: %v", err)
	}
	if _, err := w.CheckPort(context.Background(), &pb.CheckPortRequest{RemoteAddress: "127.0.0.1", Port: 3000}); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("CheckPort bypassed metadata limit: %v", err)
	}
	openAdmissionWatch(t, w)
	cancelCalls[0]()
	select {
	case <-finished[0]:
	case <-time.After(time.Second):
		t.Fatal("metadata slot not released")
	}
	if _, err := w.ListPorts(context.Background(), &pb.ListPortsRequest{}); err != nil {
		t.Errorf("released metadata capacity unavailable: %v", err)
	}
}

type pendingAdmissionScanner struct {
	started chan struct{}
	release chan struct{}
}

func (s *pendingAdmissionScanner) Scan(ctx context.Context) ([]*pb.OpenPort, error) {
	close(s.started)
	<-s.release
	return nil, ctx.Err()
}
func TestPortStopRetriesSharePendingCompletion(t *testing.T) {
	scanner := &pendingAdmissionScanner{started: make(chan struct{}), release: make(chan struct{})}
	w := admissionWorker(t, scanner)
	ended := make(chan struct{})
	var release sync.Once
	go func() { defer close(ended); _, _ = w.ListPorts(context.Background(), &pb.ListPortsRequest{}) }()
	t.Cleanup(func() {
		release.Do(func() { close(scanner.release) })
		select {
		case <-ended:
		case <-time.After(time.Second):
			t.Error("scanner did not join")
		}
	})
	select {
	case <-scanner.started:
	case <-time.After(time.Second):
		t.Fatal("scanner did not start")
	}
	for i := 0; i < 4; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		err := w.Stop(ctx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop retry %d pretended completion: %v", i, err)
		}
	}
	// Inspect the exact worker waiter stacks, not the process-wide goroutine
	// count: unrelated runtime/background goroutines must not affect the result.
	runtime.Gosched()
	trace := make([]byte, 1<<20)
	n := runtime.Stack(trace, true)
	count := strings.Count(string(trace[:n]), "/plugins/port/server.(*Worker).Stop.func")
	if count != 1 {
		t.Errorf("Stop retry left %d waiter goroutines for one pending shutdown; want 1", count)
	}
	if _, err := w.ListPorts(context.Background(), &pb.ListPortsRequest{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("stopping worker accepted new work: %v", err)
	}
	release.Do(func() { close(scanner.release) })
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("released scanner did not finish")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		if err := w.Stop(ctx); err != nil {
			t.Fatalf("completed Stop %d: %v", i, err)
		}
	}
}
