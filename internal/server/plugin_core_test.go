package server

import (
	"connectrpc.com/connect/v2"
	"context"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"testing"
)

func TestDaemonCoreRejectsClientRoutingScopes(t *testing.T) {
	core := &daemonPluginCore{}
	for _, scope := range []*ppb.Scope{nil, {ServerId: "server"}, {MappingId: "mapping"}} {
		if e := core.authorizeScope(context.Background(), scope); connect.CodeOf(e) != connect.CodePermissionDenied {
			t.Fatalf("invalid daemon scope accepted: %v %v", scope, e)
		}
	}
}
