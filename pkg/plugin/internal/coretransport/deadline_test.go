package coretransport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
)

type blockedMetadata struct {
	rpc.UnimplementedCoreAPIHandler
	entered chan context.Context
	exited  chan struct{}
}

func (p *blockedMetadata) RemoteListFiles(ctx context.Context, _ *ppb.RemoteListFilesRequest) (*pb.ListFilesResponse, error) {
	p.entered <- ctx
	<-ctx.Done()
	close(p.exited)
	return nil, ctx.Err()
}

func metadataClient(t *testing.T, handler rpc.CoreAPIHandler) rpc.CoreAPIClient {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shiki-core-deadline-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "core.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(socket, 0600); err != nil {
		listener.Close()
		t.Fatal(err)
	}
	server := connect.NewServer()
	rpc.RegisterCoreAPIHandler(server, handler)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server, connecthttp.WithCompressors())
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	httpServer := &http.Server{Handler: mux, Protocols: protocols}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	t.Cleanup(func() { httpServer.Close(); <-done })
	endpoint, err := NewUnixClient(socket, uint32(os.Getuid()), "shiki-core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(endpoint.Close)
	return rpc.NewCoreAPIClient(NewRPCClient(endpoint))
}

func TestBlockedMetadataUsesDefaultDeadline(t *testing.T) {
	probe := &blockedMetadata{entered: make(chan context.Context, 1), exited: make(chan struct{})}
	client := metadataClient(t, probe)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	start := time.Now()
	go func() { _, err := client.RemoteListFiles(ctx, &ppb.RemoteListFilesRequest{}); result <- err }()
	select {
	case <-probe.entered:
	case <-time.After(time.Second):
		t.Fatal("metadata handler never entered")
	}
	select {
	case err := <-result:
		if connect.CodeOf(err) != connect.CodeDeadlineExceeded {
			t.Fatal("blocked metadata lost deadline cause", err)
		}
		if elapsed := time.Since(start); elapsed < 4*time.Second || elapsed > 6*time.Second {
			t.Fatal("wrong default deadline", elapsed)
		}
	case <-time.After(6 * time.Second):
		cancel()
		<-result
		t.Fatal("blocked metadata survived the default 5-second RPC budget")
	}
	select {
	case <-probe.exited:
	case <-time.After(time.Second):
		t.Fatal("server did not observe deadline cancellation")
	}
}

func TestBlockedMetadataPreservesEarlierParentDeadline(t *testing.T) {
	probe := &blockedMetadata{entered: make(chan context.Context, 1), exited: make(chan struct{})}
	client := metadataClient(t, probe)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := client.RemoteListFiles(ctx, &ppb.RemoteListFilesRequest{})
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded {
		t.Fatal(err)
	}
	select {
	case <-probe.entered:
	default:
		t.Fatal("parent deadline test never reached server")
	}
	select {
	case <-probe.exited:
	case <-time.After(time.Second):
		t.Fatal("earlier parent deadline did not cancel server")
	}
}

type unaryLifecycleProbe struct {
	ctx     context.Context
	failAt  string
	failure error
}

func (p *unaryLifecycleProbe) check(phase string) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if phase == p.failAt {
		return p.failure
	}
	return nil
}
func (p *unaryLifecycleProbe) Send(any) error     { return p.check("send") }
func (p *unaryLifecycleProbe) SendHeaders() error { return p.check("headers") }
func (p *unaryLifecycleProbe) CloseSend() error   { return p.check("half-close") }
func (p *unaryLifecycleProbe) Receive(any) error  { return p.check("receive") }
func (p *unaryLifecycleProbe) Close() error       { return p.check("close") }

func TestUnaryBudgetLivesThroughResponseAndReleasesOnEveryExit(t *testing.T) {
	for _, phase := range []string{"success", "create", "send", "headers", "half-close", "receive", "close"} {
		t.Run(phase, func(t *testing.T) {
			remote := connect.NewError(connect.CodeDataLoss, "terminal failure")
			probe := &unaryLifecycleProbe{failAt: phase, failure: remote}
			start := time.Now()
			stream, err := unaryDeadline(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
				probe.ctx = ctx
				if phase == "create" {
					return nil, remote
				}
				return probe, nil
			})(t.Context(), connect.Spec{StreamType: connect.StreamTypeUnary})
			deadline, ok := probe.ctx.Deadline()
			if !ok || deadline.Before(start.Add(5*time.Second)) || deadline.After(time.Now().Add(5*time.Second)) {
				t.Fatal("wrong default deadline", deadline)
			}
			if phase != "create" {
				if err != nil {
					t.Fatal(err)
				}
				if err := probe.ctx.Err(); err != nil {
					t.Fatal("stream construction canceled budget", err)
				}
				switch phase {
				case "send":
					err = stream.Send(nil)
				case "headers":
					err = stream.SendHeaders()
				case "half-close":
					err = stream.CloseSend()
				case "close":
					err = stream.Close()
				default:
					if err = stream.SendHeaders(); err != nil {
						t.Fatal(err)
					}
					if err = stream.Send(nil); err != nil {
						t.Fatal(err)
					}
					if err = stream.CloseSend(); err != nil {
						t.Fatal(err)
					}
					err = stream.Receive(nil)
				}
			}
			if phase == "success" && err != nil || phase != "success" && !errors.Is(err, remote) {
				t.Fatal("cleanup replaced response/error", err)
			}
			if !errors.Is(probe.ctx.Err(), context.Canceled) {
				t.Fatal("unary timer/context retained after terminal operation")
			}
		})
	}
}

func TestUnaryBudgetLeavesStreamingContextsUntouched(t *testing.T) {
	for _, kind := range []connect.StreamType{connect.StreamTypeServer, connect.StreamTypeClient, connect.StreamTypeBidi} {
		ctx, cancel := context.WithCancel(t.Context())
		probe := &unaryLifecycleProbe{}
		stream, err := unaryDeadline(func(seen context.Context, _ connect.Spec) (connect.ClientStream, error) {
			if seen != ctx {
				t.Fatal("stream received a short unary budget")
			}
			probe.ctx = seen
			return probe, nil
		})(ctx, connect.Spec{StreamType: kind})
		if err != nil || stream != probe {
			t.Fatal("stream was wrapped", err)
		}
		cancel()
	}
}

func TestUnaryBudgetNeverExtendsParentDeadline(t *testing.T) {
	for _, budget := range []time.Duration{time.Second, 30 * time.Second} {
		ctx, cancel := context.WithTimeout(t.Context(), budget)
		parentDeadline, _ := ctx.Deadline()
		probe := &unaryLifecycleProbe{}
		stream, err := unaryDeadline(func(seen context.Context, _ connect.Spec) (connect.ClientStream, error) {
			probe.ctx = seen
			return probe, nil
		})(ctx, connect.Spec{StreamType: connect.StreamTypeUnary})
		if err != nil {
			t.Fatal(err)
		}
		deadline, ok := probe.ctx.Deadline()
		if !ok || deadline.After(parentDeadline) || budget == time.Second && !deadline.Equal(parentDeadline) || time.Until(deadline) > 5*time.Second {
			t.Fatal("parent deadline extended", deadline, parentDeadline)
		}
		stream.Close()
		cancel()
	}
}
