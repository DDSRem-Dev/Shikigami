package agent

import (
	"context"
	"reflect"
	"testing"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

// The reflection read keeps this exact behavior test compilable against the
// pre-change schema: a real authenticated route must be visible in ListServers.
func TestConnectionAttemptDiagnosticsReportsActualRoute(t *testing.T) {
	for _, denied := range []bool{false, true} {
		name := "success"
		if denied {
			name = "authentication-failure"
		}
		t.Run(name, func(t *testing.T) {
			endpoint := routeEndpoint(t, func(ctx context.Context) (*pb.ProtocolAgreement, error) {
				if denied {
					return nil, rpcError(connect.CodeUnauthenticated, "REJECTED")
				}
				return routeAgreement(ctx)
			})
			service, resources := openResourceService(t, t.Context(), resourceConfig(t))
			defer resources.Close()
			stored := storeRouteServer(t, service, endpoint)
			_, err := resources.Resolve(t.Context(), stored.Config.Id)
			if denied {
				if connect.CodeOf(err) != connect.CodeUnauthenticated {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			record := findServer(t, service, stored.Config.Id)
			message := record.ProtoReflect()
			var attempts []string
			if field := message.Descriptor().Fields().ByName("connection_attempts"); field != nil {
				values := message.Get(field).List()
				for i := 0; i < values.Len(); i++ {
					attempts = append(attempts, values.Get(i).String())
				}
			}
			if !reflect.DeepEqual(attempts, []string{"direct-tls"}) {
				t.Fatalf("actual direct-TLS route missing from diagnostic: %v", attempts)
			}
		})
	}
}
