package agent

import (
	"connectrpc.com/connect/v2"
	"context"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"testing"
)

type legacyIdentityClient struct {
	rpc.ShikiDaemonClient
	calls int
	id    string
	err   error
}

func (c *legacyIdentityClient) GetSystemInfo(context.Context, *pb.Empty) (*pb.SystemInfo, error) {
	c.calls++
	return &pb.SystemInfo{DaemonId: c.id}, c.err
}
func TestNegotiatedIdentityRequiresAuthenticatedLegacyFallback(t *testing.T) {
	current := &legacyIdentityClient{err: connect.NewError(connect.CodePermissionDenied, "system.info denied")}
	agreement := &pb.ProtocolAgreement{DaemonId: "daemon_current", GrantedPermissions: []string{"system.stats"}}
	id, err := negotiatedDaemonID(t.Context(), current, agreement)
	if err != nil || id != "daemon_current" || current.calls != 0 {
		t.Fatal("new negotiation required unrelated system.info", id, err)
	}
	legacy := &legacyIdentityClient{id: "daemon_legacy"}
	old := &pb.ProtocolAgreement{GrantedPermissions: []string{"session.control"}}
	id, err = negotiatedDaemonID(t.Context(), legacy, old)
	if err != nil || id != "daemon_legacy" || legacy.calls != 1 || len(old.GrantedPermissions) != 0 {
		t.Fatal("legacy identity not verified or unbound grants trusted", id, err)
	}
	denied := &legacyIdentityClient{err: connect.NewError(connect.CodePermissionDenied, "system.info denied")}
	if _, err := negotiatedDaemonID(t.Context(), denied, &pb.ProtocolAgreement{}); connect.CodeOf(err) != connect.CodePermissionDenied || denied.calls != 1 {
		t.Fatal("denied legacy fallback silently bypassed pinning", err)
	}
}
