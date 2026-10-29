package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type statsRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	denyInfo bool
}

func (s *statsRemote) GetSystemInfo(context.Context, *pb.Empty) (*pb.SystemInfo, error) {
	if s.denyInfo {
		return nil, connect.NewError(connect.CodePermissionDenied, "system.info not granted")
	}
	return &pb.SystemInfo{DaemonId: "daemon", Capabilities: []string{"stats.v1"}}, nil
}
func (p *managementProbe) WatchPluginLogs(_ context.Context, request *apb.PluginLogsRequest, stream arpc.AgentWatchPluginLogsServerStream) error {
	if p.logCheck != nil {
		if err := p.logCheck(request); err != nil {
			return err
		}
	}
	if request.Target.Side != apb.PluginTarget_LOCAL || request.Request.PluginId != "sync" {
		return connect.NewError(connect.CodeInvalidArgument, "invalid log scope")
	}
	return stream.Send(&pb.LogRecord{Timestamp: 1, Level: "info", Message: "PLUGIN_READY"})
}

func (s *statsRemote) NegotiateProtocol(_ context.Context, offer *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	return nil, connect.NewError(connect.CodePermissionDenied, "frontends must not negotiate remote protocol")
}
func (s *statsRemote) GetStats(context.Context, *pb.StatsRequest) (*pb.StatsSnapshot, error) {
	memory := int64(12345)
	return &pb.StatsSnapshot{Timestamp: 1, MemoryUsedBytes: &memory, Sessions: []*pb.SessionStats{{SessionId: "sess_one"}}}, nil
}
func (s *statsRemote) WatchStats(ctx context.Context, request *pb.StatsRequest, stream rpc.ShikiDaemonWatchStatsServerStream) error {
	if request.IntervalSeconds != 2 {
		return connect.NewError(connect.CodeInvalidArgument, "wrong interval")
	}
	for range 2 {
		snapshot, _ := s.GetStats(ctx, request)
		if err := stream.Send(snapshot); err != nil {
			return err
		}
	}
	return nil
}
func TestStatsThroughAgentPreservesUnavailableMetricsAndNDJSON(t *testing.T) {
	path := managementFixture(t, &managementProbe{remote: &statsRemote{denyInfo: true}})
	output, err := runManagement(t, path, "stats", "--server", "server")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		t.Fatal(err)
	}
	if value, exists := snapshot["cpu_percent"]; exists && value != nil {
		t.Fatalf("unavailable CPU invented: %s", output)
	}
	output, err = runManagement(t, path, "stats", "--server", "server", "--watch", "--interval", "2")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("watch must be newline-delimited JSON: %q", output)
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal("invalid JSON record", line)
		}
	}
	var plain bytes.Buffer
	a := &app{output: &plain}
	if err := a.printStats(&pb.StatsSnapshot{Sessions: []*pb.SessionStats{{SessionId: "sess_one"}}}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain.String(), "unavailable") || strings.Contains(plain.String(), "0.0%") {
		t.Fatal("plain stats invented zero metrics", plain.String())
	}
}
func TestDiagnosticsRejectInvalidFlagsBeforeIPC(t *testing.T) {
	for _, args := range [][]string{{"dashboard"}, {"stats", "--interval", "0"}, {"stats", "--interval", "61"}, {"logs", "sync"}, {"logs", "sync", "--side", "remote"}} {
		output, err := runManagement(t, "/missing/config", args...)
		if err == nil || output != "" || strings.Contains(err.Error(), "socket") {
			t.Fatalf("invalid diagnostics touched IPC: %v %q %v", args, output, err)
		}
	}
}

func TestDoctorKeepsIndependentErrorsAndLogAliasUsesIPC(t *testing.T) {
	path := managementFixture(t, &managementProbe{remote: &statsRemote{}})
	output, err := runManagement(t, path, "doctor", "--server", "server")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	failures, ok := report["errors"].(map[string]any)
	if !ok || failures["claude"] == nil || failures["health"] == nil || report["resources"] == nil || report["local_plugins"] == nil {
		t.Fatalf("one failed diagnostic hid independent results: %s", output)
	}
	output, err = runManagement(t, path, "logs", "sync", "--side", "local")
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(output), &record); err != nil || record["message"] != "PLUGIN_READY" {
		t.Fatalf("log alias: %s %v", output, err)
	}
}

func TestDoctorReportsGrantedStatsWhenSystemInfoIsDenied(t *testing.T) {
	path := managementFixture(t, &managementProbe{remote: &statsRemote{denyInfo: true}, server: &apb.ServerRecord{
		Config: &apb.ServerConfig{Id: "server", Name: "restricted", DaemonId: "daemon_restricted"}, ConnectionState: "ready", ConnectionPath: "direct-tls",
		RemoteProtocol: &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_restricted", Capabilities: []string{"stats.v1"}, GrantedPermissions: []string{"system.stats"}},
	}})
	output, err := runManagement(t, path, "doctor", "--server", "server")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err = json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report["remote_error"] == nil || report["resources"] == nil {
		t.Fatal("denied system information hid granted resource diagnostics", output)
	}
}
