package version

import (
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"
)

func TestSafeDiagnosticsRejectUntrustedMetadata(t *testing.T) {
	base := incompatible(&pb.ProtocolOffer{Major: 1}, &pb.ProtocolOffer{Major: 2}, "fixture-a", nil)
	for name, mutate := range map[string]func(*errdetails.ErrorInfo){
		"unknown-key":         func(i *errdetails.ErrorInfo) { i.Metadata["token"] = "secret" },
		"control-build":       func(i *errdetails.ErrorInfo) { i.Metadata["local_build"] = "a\nsecret" },
		"oversized-build":     func(i *errdetails.ErrorInfo) { i.Metadata["local_build"] = strings.Repeat("a", 4096) },
		"noncanonical-number": func(i *errdetails.ErrorInfo) { i.Metadata["peer_major"] = "02" },
		"overflow":            func(i *errdetails.ErrorInfo) { i.Metadata["peer_major"] = "4294967296" },
		"component":           func(i *errdetails.ErrorInfo) { i.Metadata["component"] = "secret-host" },
		"too-many-caps":       func(i *errdetails.ErrorInfo) { i.Metadata["missing_capabilities"] = strings.Repeat("x,", 8) + "x" },
		"foreign-domain":      func(i *errdetails.ErrorInfo) { i.Domain = "untrusted" },
		"foreign-reason":      func(i *errdetails.ErrorInfo) { i.Reason = "DEBUG" },
	} {
		t.Run(name, func(t *testing.T) {
			var info errdetails.ErrorInfo
			if err := proto.Unmarshal(SafeDetail(base).Value, &info); err != nil {
				t.Fatal(err)
			}
			mutate(&info)
			detail, _ := connectproto.NewErrorDetail(&info)
			if SafeDetail(connect.NewError(connect.CodeFailedPrecondition, "secret").WithDetail(detail)) != nil {
				t.Fatal("unsafe detail admitted")
			}
		})
	}
	detail := SafeDetail(base)
	detail.Debug = []byte(`{"secret":"must disappear"}`)
	safe := SafeDetail(connect.NewError(connect.CodeFailedPrecondition, "secret").WithDetail(detail))
	if safe == nil || len(safe.Debug) != 0 {
		t.Fatal("debug not stripped")
	}
	if SafeDetail(connect.NewError(connect.CodeInternal, "secret").WithDetail(detail)) != nil {
		t.Fatal("wrong status admitted")
	}
}

func TestMissingCapabilityDiagnosticHasIndependentSchema(t *testing.T) {
	err := MissingRemoteCapability("stats.v1")
	if got := Describe(err, "fixture"); !strings.Contains(got, "daemon") || !strings.Contains(got, "stats.v1") {
		t.Fatal(got)
	}
	var info errdetails.ErrorInfo
	if err := proto.Unmarshal(SafeDetail(err).Value, &info); err != nil {
		t.Fatal(err)
	}
	info.Metadata["local_build"] = "secret"
	detail, _ := connectproto.NewErrorDetail(&info)
	if SafeDetail(connect.NewError(connect.CodeFailedPrecondition, "secret").WithDetail(detail)) != nil {
		t.Fatal("foreign diagnostic keys admitted")
	}
	if SafeDetail(MissingRemoteCapability("stats\nsecret")) != nil {
		t.Fatal("invalid capability admitted")
	}
}
