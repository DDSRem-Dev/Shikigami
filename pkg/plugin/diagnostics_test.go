package plugin

import (
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

func TestProtocolDiagnosticsSurviveSafeGRPCRoundTrip(t *testing.T) {
	_, err := version.Negotiate(&pb.ProtocolOffer{Major: 1}, &pb.ProtocolOffer{Major: 2}, "synthetic-worker")
	err = version.WithComponent(err, "plugin")
	for _, source := range []error{err, version.MissingRemoteCapability("stats.v1")} {
		result := FromGRPC(ToGRPC(source))
		if version.SafeDetail(result) == nil || version.Describe(result, "cli") != version.Describe(source, "cli") {
			t.Fatalf("diagnostic lost: %v", result)
		}
	}
	debug, _ := connectproto.NewErrorDetail(&errdetails.DebugInfo{Detail: "secret-debug"})
	unsafe := connect.NewError(connect.CodeFailedPrecondition, "secret-token").WithDetail(debug)
	result := FromGRPC(ToGRPC(unsafe))
	if strings.Contains(result.Error(), "secret") || version.SafeDetail(result) != nil {
		t.Fatal("untrusted data escaped")
	}
}
