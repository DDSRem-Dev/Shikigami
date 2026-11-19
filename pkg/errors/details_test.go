package errors_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	rpcerrors "github.com/DDSRem-Dev/Shikigami/pkg/errors"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"
)

func TestTargetRefusedCannotHideCancellation(t *testing.T) {
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			wantCode := connect.CodeCanceled
			if sentinel == context.DeadlineExceeded {
				wantCode = connect.CodeDeadlineExceeded
			}
			for name, err := range map[string]error{
				"cause":               rpcerrors.PortTargetRefused().WithCause(sentinel),
				"wrapped":             fmt.Errorf("outer: %w", rpcerrors.PortTargetRefused().WithCause(sentinel)),
				"joined-marker-first": errors.Join(rpcerrors.PortTargetRefused(), sentinel),
				"joined-cancel-first": errors.Join(sentinel, rpcerrors.PortTargetRefused()),
			} {
				t.Run(name, func(t *testing.T) {
					if rpcerrors.IsPortTargetRefused(err) || rpcerrors.SafeDetail(err) != nil {
						t.Fatal("target refusal masked cancellation")
					}
					rebuilt := rpcerrors.RebuildError(err)
					var failure *connect.Error
					if !errors.As(rebuilt, &failure) || failure.Code() != wantCode || len(failure.Details()) != 0 {
						t.Fatalf("cancellation code/detail mismatch: %v", rebuilt)
					}
				})
			}
		})
	}
}

func TestTargetRefusedDiagnosticIsStrictAndBounded(t *testing.T) {
	valid := &errdetails.ErrorInfo{Domain: "shiki.port", Reason: "TARGET_CONNECTION_REFUSED"}
	encode := func(info *errdetails.ErrorInfo) *connect.ErrorDetail {
		detail, err := connectproto.NewErrorDetail(info)
		if err != nil {
			t.Fatal(err)
		}
		return detail
	}
	with := func(change func(*errdetails.ErrorInfo)) *connect.ErrorDetail {
		info := proto.Clone(valid).(*errdetails.ErrorInfo)
		change(info)
		return encode(info)
	}
	for _, tt := range []struct {
		name     string
		code     connect.Code
		detail   *connect.ErrorDetail
		position int
		want     bool
	}{
		{"valid", connect.CodeUnavailable, encode(valid), 0, true},
		{"eighth", connect.CodeUnavailable, encode(valid), 7, true},
		{"ninth", connect.CodeUnavailable, encode(valid), 8, false},
		{"wrong-code", connect.CodePermissionDenied, encode(valid), 0, false},
		{"wrong-domain", connect.CodeUnavailable, with(func(i *errdetails.ErrorInfo) { i.Domain = "private" }), 0, false},
		{"wrong-reason", connect.CodeUnavailable, with(func(i *errdetails.ErrorInfo) { i.Reason = "OTHER" }), 0, false},
		{"metadata", connect.CodeUnavailable, with(func(i *errdetails.ErrorInfo) { i.Metadata = map[string]string{"target": "private"} }), 0, false},
		{"unknown-fields", connect.CodeUnavailable, with(func(i *errdetails.ErrorInfo) { i.ProtoReflect().SetUnknown([]byte{0x20, 0x01}) }), 0, false},
		{"oversized", connect.CodeUnavailable, with(func(i *errdetails.ErrorInfo) { i.Metadata = map[string]string{"target": strings.Repeat("x", 2048)} }), 0, false},
		{"malformed", connect.CodeUnavailable, &connect.ErrorDetail{Type: "google.rpc.ErrorInfo", Value: []byte{0xff}}, 0, false},
		{"wrong-type", connect.CodeUnavailable, &connect.ErrorDetail{Type: "google.rpc.DebugInfo", Value: encode(valid).Value}, 0, false},
		{"message-only", connect.CodeUnavailable, nil, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := connect.NewError(tt.code, "TARGET_CONNECTION_REFUSED private text")
			for range tt.position {
				err = err.WithDetail(&connect.ErrorDetail{Type: "unapproved", Value: []byte("private")})
			}
			err = err.WithDetail(tt.detail)
			wrapped := fmt.Errorf("outer private text: %w", err)
			if got := rpcerrors.IsPortTargetRefused(wrapped); got != tt.want {
				t.Fatalf("classification = %v, want %v", got, tt.want)
			}
			rebuilt := rpcerrors.RebuildError(wrapped)
			if got := rpcerrors.IsPortTargetRefused(rebuilt); got != tt.want {
				t.Fatalf("rebuilt classification = %v, want %v", got, tt.want)
			}
			if strings.Contains(rebuilt.Error(), "private") {
				t.Fatalf("unsafe message: %v", rebuilt)
			}
			detail := rpcerrors.SafeDetail(rebuilt)
			if !tt.want {
				if detail != nil {
					t.Fatal("unapproved detail survived")
				}
				return
			}
			var info errdetails.ErrorInfo
			if detail == nil || proto.Unmarshal(detail.Value, &info) != nil || !proto.Equal(valid, &info) {
				t.Fatalf("not a canonical marker: %v", detail)
			}
		})
	}
}

func TestExistingProtocolDiagnosticSurvives(t *testing.T) {
	err := version.MissingRemoteCapability("port.forward.v1")
	before := version.SafeDetail(err)
	after := rpcerrors.SafeDetail(rpcerrors.RebuildError(err))
	if before == nil || after == nil || string(before.Value) != string(after.Value) {
		t.Fatal("lost protocol diagnostic")
	}
	if rpcerrors.IsPortTargetRefused(err) {
		t.Fatal("protocol diagnostic treated as target refusal")
	}
}
