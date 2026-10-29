package server

import (
	"connectrpc.com/connect/v2"
	"context"
	"errors"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/protobuf/types/known/structpb"
	"net"
	"testing"
	"time"
)

func TestStructuredScannerParsers(t *testing.T) {
	p, err := parseSS([]byte("LISTEN 0 128 127.0.0.1:3000 0.0.0.0:* users:((\"node\",pid=42,fd=10))\n"), false)
	if err != nil || len(p) != 1 || p[0].Pid != 42 || p[0].SessionId != "" || p[0].BindAddress != "127.0.0.1" {
		t.Fatal(p, err)
	}
	p, err = parseSS([]byte("LISTEN 0 128 [::1]:3000 [::]:*\n"), true)
	if err != nil || p[0].BindAddress != "::1" || p[0].Pid != 0 {
		t.Fatal(p, err)
	}
	p, err = parseLsof([]byte("p42\x00cnode\x00\nf7\x00tIPv4\x00n*:3000\x00\nf8\x00tIPv6\x00n*:3000\x00\n"))
	if err != nil || len(p) != 2 || p[0].BindAddress != "0.0.0.0" || p[1].BindAddress != "::" {
		t.Fatal(p, err)
	}
	if _, err = parseSS([]byte("malformed\n"), false); err == nil {
		t.Fatal("malformed ss became empty scan")
	}
	if _, err = parseLsof([]byte("p42\x00n*:3000\x00")); err == nil {
		t.Fatal("missing address family accepted")
	}
}
func TestActualSystemScannerFindsLoopbackListener(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ports, err := (SystemScanner{}).Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port := int32(listener.Addr().(*net.TCPAddr).Port)
	for _, p := range ports {
		if p.Port == port && p.BindAddress == "127.0.0.1" {
			if p.SessionId != "" {
				t.Fatal("invented session owner")
			}
			return
		}
	}
	t.Fatalf("listener %d not discovered", port)
}

type dummyCore struct{ rpc.CoreAPIClient }
type failingScanner struct{ calls int }

func (s *failingScanner) Scan(context.Context) ([]*pb.OpenPort, error) {
	s.calls++
	if s.calls > 1 {
		return nil, errors.New("scanner failed")
	}
	return []*pb.OpenPort{{BindAddress: "127.0.0.1", Port: 3000, Protocol: "tcp"}}, nil
}
func TestScanFailureDoesNotEmitClosedEvents(t *testing.T) {
	scanner := &failingScanner{}
	w := NewWithScanner(scanner)
	cfg, _ := structpb.NewStruct(map[string]any{"scan_interval_seconds": 1})
	if err := w.Init(context.Background(), sdk.InitContext{Side: "server", StateDir: t.TempDir(), Core: dummyCore{}, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer w.Stop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	count := 0
	err := w.WatchPorts(ctx, &pb.WatchPortsRequest{}, func(e *pb.PortEvent) error {
		count++
		if e.Type != pb.PortEvent_OPENED {
			t.Fatal("scanner failure emitted deletion")
		}
		return nil
	})
	if err == nil || count != 1 {
		t.Fatal(count, err)
	}
}

type pendingConnect struct {
	ctx     context.Context
	started chan struct{}
	done    chan struct{}
}

func (s *pendingConnect) Send(*pb.PortData) error { return nil }
func (s *pendingConnect) Receive() (*pb.PortData, error) {
	close(s.started)
	defer close(s.done)
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}
func TestStopCancelsStreamBeforeConnectFrame(t *testing.T) {
	w := New()
	if err := w.Init(context.Background(), sdk.InitContext{Side: "server", StateDir: t.TempDir(), Core: dummyCore{}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	transport, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &pendingConnect{ctx: transport, started: make(chan struct{}), done: make(chan struct{})}
	ended := make(chan error, 1)
	go func() { err := w.ForwardPort(transport, s); cancel(); ended <- err }()
	<-s.started
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := w.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("forward handler remained active")
	}
	select {
	case <-s.done:
	case <-ctx.Done():
		t.Fatal("transport receive remained active")
	}
}

type blockedScanner struct{ started chan struct{} }

func (s *blockedScanner) Scan(ctx context.Context) ([]*pb.OpenPort, error) {
	s.started <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestConcurrentScansAreBoundedAndJoinedOnStop(t *testing.T) {
	scan := &blockedScanner{started: make(chan struct{}, 32)}
	w := NewWithScanner(scan)
	if err := w.Init(context.Background(), sdk.InitContext{Side: "server", StateDir: t.TempDir(), Core: dummyCore{}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() {
			ended <- w.WatchPorts(context.Background(), &pb.WatchPortsRequest{}, func(*pb.PortEvent) error { return nil })
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for i := 0; i < 32; i++ {
		select {
		case <-scan.started:
		case <-ctx.Done():
			t.Fatal("scanner start stalled")
		}
	}
	err := w.WatchPorts(context.Background(), &pb.WatchPortsRequest{}, func(*pb.PortEvent) error { return nil })
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal("unbounded port scanner work", err)
	}
	if err = w.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		select {
		case err := <-ended:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("scan did not join")
		}
	}
}
