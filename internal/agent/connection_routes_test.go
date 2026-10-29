package agent

import (
	"context"
	"encoding/pem"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type routeProbe struct {
	rpc.UnimplementedShikiDaemonHandler
	probe func(context.Context) (*pb.ProtocolAgreement, error)
}

func (p *routeProbe) NegotiateProtocol(ctx context.Context, _ *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	info, _ := connect.CallInfoForServerContext(ctx)
	if info.RequestHeader().Get("Authorization") != "Bearer shiki_route_fixture" {
		return nil, rpcError(connect.CodeUnauthenticated, "WRONG_CREDENTIAL")
	}
	return p.probe(ctx)
}

func routeEndpoint(t *testing.T, probe func(context.Context) (*pb.ProtocolAgreement, error)) *apb.ServerConfig {
	t.Helper()
	server := connect.NewServer()
	rpc.RegisterShikiDaemonHandler(server, &routeProbe{probe: probe})
	httpServer := httptest.NewUnstartedServer(connection.NewRPCHandler(server))
	httpServer.EnableHTTP2 = true
	httpServer.StartTLS()
	t.Cleanup(httpServer.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: httpServer.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	return &apb.ServerConfig{DirectTlsAddress: httpServer.Listener.Addr().String(), TlsServerName: "example.com", TlsCaFile: ca}
}

func routeAgreement(context.Context) (*pb.ProtocolAgreement, error) {
	return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_route", GrantedPermissions: []string{"system.stats"}}, nil
}

func TestRouteSelectionAuthenticatesBeforeChoosingAndClosesLosers(t *testing.T) {
	good := routeEndpoint(t, routeAgreement)
	for _, test := range []struct {
		name string
		mode string
		want []string
		ok   bool
	}{
		{"tsnet wins", "good", []string{"tsnet"}, true},
		{"network fallback", "network", []string{"tsnet", "direct-tls"}, true},
		{"unauthenticated stops", "auth", []string{"tsnet"}, false},
		{"permission stops", "permission", []string{"tsnet"}, false},
		{"certificate stops", "certificate", []string{"tsnet"}, false},
		{"identity stops", "identity", []string{"tsnet"}, false},
		{"protocol stops", "protocol", []string{"tsnet"}, false},
		{"remote deadline stops", "deadline", []string{"tsnet"}, false},
		{"remote cancellation stops", "cancel", []string{"tsnet"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			primary := good
			switch test.mode {
			case "network":
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				primary = &apb.ServerConfig{DirectTlsAddress: listener.Addr().String(), TlsServerName: "example.com", TlsCaFile: good.TlsCaFile}
				listener.Close()
			case "certificate":
				primary = &apb.ServerConfig{DirectTlsAddress: good.DirectTlsAddress, TlsServerName: "wrong.invalid", TlsCaFile: good.TlsCaFile}
			case "auth", "permission", "identity", "protocol", "deadline", "cancel":
				primary = routeEndpoint(t, func(context.Context) (*pb.ProtocolAgreement, error) {
					switch test.mode {
					case "auth":
						return nil, rpcError(connect.CodeUnauthenticated, "REJECTED")
					case "permission":
						return nil, rpcError(connect.CodePermissionDenied, "DENIED")
					case "identity":
						return &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_other"}, nil
					case "deadline":
						return nil, rpcError(connect.CodeDeadlineExceeded, "REMOTE_REQUEST_DEADLINE")
					case "cancel":
						return nil, rpcError(connect.CodeCanceled, "REMOTE_REQUEST_CANCELED")
					default:
						return &pb.ProtocolAgreement{Major: 2, DaemonId: "daemon_route"}, nil
					}
				})
			}
			var order []string
			closed := map[string]int{}
			r := &Resources{}
			candidate, reported, err := establishRemote(t.Context(), &apb.ServerConfig{TailscaleAddress: "node:9527", DirectTlsAddress: good.DirectTlsAddress, DaemonId: "daemon_route"}, func(route string) (*connect.Client, func(), error) {
				order = append(order, route)
				config := primary
				if route == "direct-tls" {
					config = good
				}
				base, closePool, err := r.newTrustedRoute(config, "shiki_route_fixture", "direct-tls")
				return base, func() { closed[route]++; closePool() }, err
			})
			if !reflect.DeepEqual(order, test.want) || !reflect.DeepEqual(reported, order) || (err == nil) != test.ok {
				t.Fatalf("routes %v, error %v", order, err)
			}
			if candidate != nil {
				if candidate.daemonID != "daemon_route" || closed[candidate.route] != 0 {
					t.Fatal("selected route was not authenticated or was already closed")
				}
				candidate.close()
			}
			for _, route := range order {
				if closed[route] != 1 {
					t.Fatalf("candidate %s closed %d times", route, closed[route])
				}
			}
		})
	}
}

func TestRouteDeadlineReservesFallbackButCallerCancellationStops(t *testing.T) {
	good := routeEndpoint(t, routeAgreement)
	release := make(chan struct{})
	defer close(release) // Join blackhole handlers before httptest cleanup.
	slow := routeEndpoint(t, func(ctx context.Context) (*pb.ProtocolAgreement, error) {
		// Returning on the server's delegated deadline can send a remote
		// application error before the client's timer expires. This case
		// requires a local probe timeout, so send no response until teardown.
		<-release
		return nil, ctx.Err()
	})
	r := &Resources{}
	var attempts atomic.Int32
	factory := func(route string) (*connect.Client, func(), error) {
		attempts.Add(1)
		config := slow
		if route == "direct-tls" {
			config = good
		}
		return r.newTrustedRoute(config, "shiki_route_fixture", "direct-tls")
	}
	server := &apb.ServerConfig{TailscaleAddress: "node:9527", DirectTlsAddress: good.DirectTlsAddress, DaemonId: "daemon_route"}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	candidate, reported, err := establishRemote(ctx, server, factory)
	if err != nil || candidate.route != "direct-tls" || attempts.Load() != 2 || !reflect.DeepEqual(reported, []string{"tsnet", "direct-tls"}) {
		t.Fatalf("sub-deadline consumed fallback budget: %v, attempts %d", err, attempts.Load())
	}
	candidate.close()
	ctx2, cancel2 := context.WithCancel(t.Context())
	canceled := routeEndpoint(t, func(context.Context) (*pb.ProtocolAgreement, error) {
		cancel2()
		return nil, context.Canceled
	})
	attempts.Store(0)
	_, reported, err = establishRemote(ctx2, server, func(string) (*connect.Client, func(), error) {
		attempts.Add(1)
		return r.newTrustedRoute(canceled, "shiki_route_fixture", "direct-tls")
	})
	if !errors.Is(err, context.Canceled) || attempts.Load() != 1 || !reflect.DeepEqual(reported, []string{"tsnet"}) {
		t.Fatalf("caller cancellation triggered another route: %v, attempts %d", err, attempts.Load())
	}
}
