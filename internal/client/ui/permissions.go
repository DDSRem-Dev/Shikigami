package ui

import (
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"slices"
)

// The authenticated daemon's expanded grants are a presentation hint only.
// Unknown/legacy permissions hide privileged actions; RPC authorization remains
// authoritative even if cached hints become stale after credential replacement.
func granted(agreement *pb.ProtocolAgreement, permissions ...string) bool {
	if agreement.GetDaemonId() == "" {
		return false
	}
	for _, permission := range permissions {
		if !slices.Contains(agreement.GetGrantedPermissions(), permission) {
			return false
		}
	}
	return true
}
func (m *Model) allowed(permissions ...string) bool { return granted(m.agreement, permissions...) }
func (m *Model) canObserve() bool {
	return m.agreement.GetDaemonId() == "" || m.allowed("session.attach")
}
func (m *Model) canCreate() bool {
	return m.allowed("session.create") && (m.projectID != "" || m.allowed("project.list"))
}
func (m *Model) selectedAgreement() {
	m.agreement = nil
	for _, server := range m.servers {
		if server.GetConfig().GetId() == m.serverID {
			m.agreement = server.RemoteProtocol
			return
		}
	}
}
