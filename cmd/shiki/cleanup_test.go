package main

import (
	"context"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"strings"
	"testing"
	"time"
)

type cleanupRemote struct{ statsRemote }

func (*cleanupRemote) ListSessions(context.Context, *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	warning, due := time.Now().Unix(), time.Now().Add(time.Hour).Unix()
	return &pb.ListSessionsResponse{Sessions: []*pb.Session{{Id: "sess_cleanup", CleanupStatus: "warned", CleanupWarningAt: &warning, CleanupAfter: &due}}}, nil
}
func TestDoctorCleanupWarningsNeedOnlySessionList(t *testing.T) {
	path := managementFixture(t, &managementProbe{remote: &cleanupRemote{statsRemote: statsRemote{denyInfo: true}}, server: &apb.ServerRecord{Config: &apb.ServerConfig{Id: "server", Name: "restricted", DaemonId: "daemon_restricted"}, ConnectionState: "ready", RemoteProtocol: &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_restricted", GrantedPermissions: []string{"session.list"}}}})
	out, err := runManagement(t, path, "doctor", "--server", "server")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sess_cleanup") || !strings.Contains(out, "Cleanup after") || !strings.Contains(out, "remote_error") {
		t.Fatal("denied system.info hid cleanup warning", out)
	}
}
