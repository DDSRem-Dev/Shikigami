package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
)

func TestConnectionAttemptsOnlyCountEnteredRouteFactories(t *testing.T) {
	for _, tc := range []struct {
		name     string
		server   *apb.ServerConfig
		canceled bool
		want     []string
	}{
		{"both-network-failures", &apb.ServerConfig{TailscaleAddress: "private-node:9527", DirectTlsAddress: "private-direct:9527"}, false, []string{"tsnet", "direct-tls"}},
		{"direct-only", &apb.ServerConfig{DirectTlsAddress: "private-direct:9527"}, false, []string{"direct-tls"}},
		{"no-route", &apb.ServerConfig{}, false, nil},
		{"already-canceled", &apb.ServerConfig{TailscaleAddress: "private-node:9527", DirectTlsAddress: "private-direct:9527"}, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			var entered []string
			candidate, attempts, err := establishRemote(ctx, tc.server, func(route string) (*connect.Client, func(), error) {
				entered = append(entered, route)
				return nil, nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
			})
			if candidate != nil || err == nil || !reflect.DeepEqual(entered, tc.want) || !reflect.DeepEqual(attempts, entered) {
				t.Fatal(candidate, attempts, entered, err)
			}
		})
	}
}

func TestConnectionAttemptsAreRuntimeOnlyAndSnapshotsAreIndependent(t *testing.T) {
	endpoint := routeEndpoint(t, routeAgreement)
	service, r := openResourceService(t, t.Context(), resourceConfig(t))
	defer r.Close()
	record := storeRouteServer(t, service, endpoint)
	if _, err := r.Resolve(t.Context(), record.Config.Id); err != nil {
		t.Fatal(err)
	}
	observed := findServer(t, service, record.Config.Id)
	if !reflect.DeepEqual(observed.ConnectionAttempts, []string{"direct-tls"}) {
		t.Fatal(observed)
	}
	observed.ConnectionAttempts[0] = "forged-private-address"
	if got := findServer(t, service, record.Config.Id); !reflect.DeepEqual(got.ConnectionAttempts, []string{"direct-tls"}) {
		t.Fatal("returned slice aliases runtime state", got)
	}
	// Successful verification of an existing route is not another route selection.
	if _, err := r.Resolve(t.Context(), record.Config.Id); err != nil {
		t.Fatal(err)
	}
	if got := findServer(t, service, record.Config.Id); !reflect.DeepEqual(got.ConnectionAttempts, []string{"direct-tls"}) {
		t.Fatal("verification reset or accumulated attempts", got)
	}
	snapshot, err := r.store.View()
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := storedServer(snapshot, record.Config.Id)
	if err != nil || hasServerRuntime(persisted) {
		t.Fatal("runtime fields persisted", persisted, err)
	}
	for key, receipt := range snapshot.Requests {
		if !strings.HasPrefix(key, "UpsertServer\x00") && !strings.HasPrefix(key, "SetServerCredential\x00") {
			continue
		}
		result := new(apb.ServerRecord)
		if err := json.Unmarshal(receipt.Result, result); err != nil || hasServerRuntime(result) {
			t.Fatal("runtime result persisted", key, result, err)
		}
	}
	// A result copied into any deduplication namespace must be stripped before
	// serialization, even if a future mutation accidentally returns a decorated record.
	r.mu.Lock()
	_, err = r.mutate("RuntimeResultRegression", "result", &pb.Empty{}, observed, func(*storage.Snapshot) error { return nil })
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = r.store.View()
	if err != nil {
		t.Fatal(err)
	}
	var saved apb.ServerRecord
	if err := json.Unmarshal(snapshot.Requests["RuntimeResultRegression\x00result"].Result, &saved); err != nil || hasServerRuntime(&saved) {
		t.Fatal("dedup did not strip runtime result", &saved, err)
	}
	if err := r.store.Update(func(s *storage.Snapshot) error {
		copy := proto.Clone(persisted).(*apb.ServerRecord)
		copy.ConnectionAttempts = []string{"tsnet"}
		return s.Put("servers", record.Config.Id, copy)
	}); err == nil {
		t.Fatal("persistent server accepted runtime attempts")
	}
	if err := r.store.Update(func(s *storage.Snapshot) error {
		for key, receipt := range s.Requests {
			if strings.HasPrefix(key, "UpsertServer\x00") {
				copy := proto.Clone(persisted).(*apb.ServerRecord)
				copy.ConnectionAttempts = []string{"tsnet"}
				receipt.Result, _ = json.Marshal(copy)
				s.Requests[key] = receipt
				return nil
			}
		}
		return errors.New("missing receipt")
	}); err == nil {
		t.Fatal("persistent server receipt accepted runtime attempts")
	}
	empty := &Resources{pools: map[string]*remotePool{}}
	stale := proto.Clone(observed).(*apb.ServerRecord)
	empty.decorateLocked(stale)
	if len(stale.ConnectionAttempts) != 0 {
		t.Fatal("new runtime inferred old observations", stale)
	}
}

func TestConnectionAttemptsMissingCredentialDoesNotInventRoutes(t *testing.T) {
	var probes atomic.Int32
	endpoint := routeEndpoint(t, func(ctx context.Context) (*pb.ProtocolAgreement, error) { probes.Add(1); return routeAgreement(ctx) })
	endpoint.Name = "without-credential"
	service, r := openResourceService(t, t.Context(), resourceConfig(t))
	defer r.Close()
	record, err := service.UpsertServer(t.Context(), &apb.UpsertServerRequest{Config: endpoint, RequestId: newID("request_")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve(t.Context(), record.Config.Id); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatal(err)
	}
	if got := findServer(t, service, record.Config.Id); len(got.ConnectionAttempts) != 0 || probes.Load() != 0 {
		t.Fatal("credential failure invented a route attempt", got, probes.Load())
	}
}

func TestConnectionAttemptsObsoleteSelectionCannotPublish(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	entered := make(chan struct{}, 1)
	old := routeEndpoint(t, func(call context.Context) (*pb.ProtocolAgreement, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-call.Done()
		return nil, call.Err()
	})
	service, r := openResourceService(t, ctx, resourceConfig(t))
	defer r.Close()
	record := storeRouteServer(t, service, old)
	result := make(chan error, 1)
	go func() { _, err := r.Resolve(ctx, record.Config.Id); result <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("old route did not enter", ctx.Err())
	}
	current := findServer(t, service, record.Config.Id)
	changed := proto.Clone(current.Config).(*apb.ServerConfig)
	changed.TailscaleAddress = "node:9527"
	changed.DirectTlsAddress = ""
	changed.TlsCaFile = ""
	changed.TlsServerName = ""
	if _, err := service.UpsertServer(ctx, &apb.UpsertServerRequest{Config: changed, ExpectedRevision: current.Revision, RequestId: newID("request_")}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("obsolete selection succeeded")
		}
	case <-ctx.Done():
		t.Fatal("obsolete selection did not stop")
	}
	// The new generation actually enters tsnet's factory, which refuses the
	// unavailable node before network I/O. It must not inherit old direct-TLS data.
	if _, err := r.Resolve(ctx, record.Config.Id); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal(err)
	}
	if got := findServer(t, service, record.Config.Id); !reflect.DeepEqual(got.ConnectionAttempts, []string{"tsnet"}) {
		t.Fatal("old generation contaminated diagnostics", got)
	}
}
