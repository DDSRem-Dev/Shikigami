package errors

import (
	"context"
	"errors"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"
)

const portDomain = "shiki.port"
const targetRefused = "TARGET_CONNECTION_REFUSED"

// PortTargetRefused identifies an authorized loopback target's ECONNREFUSED,
// before CONNECTED. It carries no address or other peer-controlled metadata.
func PortTargetRefused() *connect.Error {
	return connect.NewError(connect.CodeUnavailable, targetRefused).WithDetail(targetRefusedDetail())
}

func targetRefusedDetail() *connect.ErrorDetail {
	detail, _ := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Domain: portDomain, Reason: targetRefused})
	return detail
}

// IsPortTargetRefused requires the complete bounded diagnostic, never message
// matching or the generic Unavailable code alone.
func IsPortTargetRefused(err error) bool {
	// Cancellation wins over a diagnostic elsewhere in a wrapped/joined chain,
	// matching RebuildError's code precedence at every proxy boundary.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var failure *connect.Error
	if !errors.As(err, &failure) || failure.Code() != connect.CodeUnavailable {
		return false
	}
	for index, detail := range failure.Details() {
		if index >= 8 {
			break
		}
		if detail == nil || detail.Type != "google.rpc.ErrorInfo" || len(detail.Value) > 2048 {
			continue
		}
		var info errdetails.ErrorInfo
		if proto.Unmarshal(detail.Value, &info) == nil && info.Domain == portDomain && info.Reason == targetRefused && len(info.Metadata) == 0 && len(info.ProtoReflect().GetUnknown()) == 0 {
			return true
		}
	}
	return false
}

// SafeDetail admits one canonical diagnostic from the protocol or port schema.
// All other details and metadata are discarded at proxy boundaries.
func SafeDetail(err error) *connect.ErrorDetail {
	if IsPortTargetRefused(err) {
		return targetRefusedDetail()
	}
	return version.SafeDetail(err)
}
