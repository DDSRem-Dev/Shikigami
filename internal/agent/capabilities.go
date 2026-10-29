package agent

import (
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

// Only explicitly versioned optional methods require a capability. Core
// Project/Session CRUD and identity/health methods remain protocol-1 baseline.
var remoteMethodCapabilities = map[string]string{
	rpc.ShikiDaemonTerminalStreamProcedure:         "terminal.v1",
	rpc.ShikiDaemonTakeControlProcedure:            "terminal.v1",
	rpc.ShikiDaemonGetSessionScreenProcedure:       "terminal.screen.v1",
	rpc.ShikiDaemonGetSessionLogsProcedure:         "terminal.logs.v1",
	rpc.ShikiDaemonListProjectDirectoriesProcedure: "project.directories.v1",
	rpc.ShikiDaemonCreateSnapshotProcedure:         "session-config-snapshot.v1",
	rpc.ShikiDaemonRestoreSnapshotProcedure:        "session-config-snapshot.v1",
	rpc.ShikiDaemonListSnapshotsProcedure:          "session-config-snapshot.v1",
	rpc.ShikiDaemonDeleteSnapshotProcedure:         "session-config-snapshot.v1",
	rpc.ShikiDaemonGetStatsProcedure:               "stats.v1",
	rpc.ShikiDaemonWatchStatsProcedure:             "stats.v1",
	rpc.ShikiDaemonListPluginsProcedure:            "plugin.manage.v1",
	rpc.ShikiDaemonManagePluginProcedure:           "plugin.manage.v1",
	rpc.ShikiDaemonWatchPluginLogsProcedure:        "plugin.manage.v1",
	rpc.ShikiDaemonListFilesProcedure:              sdk.FileCapability,
	rpc.ShikiDaemonStatFileProcedure:               sdk.FileCapability,
	rpc.ShikiDaemonReadFileProcedure:               sdk.FileCapability,
	rpc.ShikiDaemonWriteFileProcedure:              sdk.FileCapability,
	rpc.ShikiDaemonDeleteFileProcedure:             sdk.FileCapability,
	rpc.ShikiDaemonWatchFilesProcedure:             sdk.FileCapability,
	rpc.ShikiDaemonListPortsProcedure:              sdk.PortCapability,
	rpc.ShikiDaemonWatchPortsProcedure:             sdk.PortCapability,
	rpc.ShikiDaemonForwardPortProcedure:            sdk.PortCapability,
	rpc.ShikiDaemonCheckPortProcedure:              sdk.PortCheckCapability,
}

type remoteCapabilities struct{ values map[string]bool }

func (p *remotePool) setCapabilities(agreement *pb.ProtocolAgreement) {
	available := &remoteCapabilities{values: map[string]bool{}}
	for _, capability := range agreement.GetCapabilities() {
		available.values[capability] = true
	}
	p.capabilities.Store(available)
}
func (p *remotePool) requireCapability(procedure string) error {
	required := remoteMethodCapabilities[procedure]
	if required == "" {
		return nil
	}
	available := p.capabilities.Load()
	if available == nil || !available.values[required] {
		return version.MissingRemoteCapability(required)
	}
	return nil
}
