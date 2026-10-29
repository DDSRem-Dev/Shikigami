package client

import (
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
	"strings"
)

// ConnectionRecovery gives a concrete frontend action without exposing or
// inventing credentials, changing trust, or resetting an episode implicitly.
func ConnectionRecovery(record *apb.ServerRecord) string {
	id := record.GetConfig().GetId()
	if id == "" {
		return ""
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return "Inspect the saved server with shiki servers list."
		}
	}
	switch {
	case record.ReasonCode == "RECONNECT_EXHAUSTED":
		return "Check the network, then retry: shiki connect " + id
	case record.ConnectionState == "auth_required":
		return "Provide a valid Token: shiki connect " + id + " --token-file /absolute/path/to/token"
	case record.ConnectionState == "incompatible":
		return "Inspect the configured identity and trust: shiki config get --server " + id
	default:
		return ""
	}
}

// ConnectionAttemptRoutes accepts only the bounded ordered route vocabulary.
// Empty/old/invalid diagnostics do not imply that no route was attempted.
func ConnectionAttemptRoutes(record *apb.ServerRecord) []string {
	routes := record.GetConnectionAttempts()
	if len(routes) == 1 && (routes[0] == "tsnet" || routes[0] == "direct-tls") {
		return []string{routes[0]}
	}
	if len(routes) == 2 && routes[0] == "tsnet" && routes[1] == "direct-tls" {
		return []string{"tsnet", "direct-tls"}
	}
	return nil
}
func ConnectionAttemptsNotice(record *apb.ServerRecord) string {
	routes := ConnectionAttemptRoutes(record)
	if len(routes) == 0 {
		return "Latest route selection: not reported"
	}
	return "Latest route selection: " + strings.Join(routes, " → ")
}

// SafeConnectionAttemptList preserves the server model while dropping unknown
// diagnostic values before either human-readable or structured presentation.
func SafeConnectionAttemptList(list *apb.ServerList) *apb.ServerList {
	if list == nil {
		return nil
	}
	copy := proto.Clone(list).(*apb.ServerList)
	for _, record := range copy.Servers {
		if record != nil {
			record.ConnectionAttempts = ConnectionAttemptRoutes(record)
		}
	}
	return copy
}
