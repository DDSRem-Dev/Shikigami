package plugin

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect/v2"
	rpcerrors "github.com/DDSRem-Dev/Shikigami/pkg/errors"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

var grpcToConnect = map[codes.Code]connect.Code{
	codes.Canceled: connect.CodeCanceled, codes.Unknown: connect.CodeUnknown, codes.InvalidArgument: connect.CodeInvalidArgument,
	codes.DeadlineExceeded: connect.CodeDeadlineExceeded, codes.NotFound: connect.CodeNotFound, codes.AlreadyExists: connect.CodeAlreadyExists,
	codes.PermissionDenied: connect.CodePermissionDenied, codes.ResourceExhausted: connect.CodeResourceExhausted,
	codes.FailedPrecondition: connect.CodeFailedPrecondition, codes.Aborted: connect.CodeAborted, codes.OutOfRange: connect.CodeOutOfRange,
	codes.Unimplemented: connect.CodeUnimplemented, codes.Internal: connect.CodeInternal, codes.Unavailable: connect.CodeUnavailable,
	codes.DataLoss: connect.CodeDataLoss, codes.Unauthenticated: connect.CodeUnauthenticated,
}

// FromGRPC reconstructs a safe, local Connect error at the worker boundary.
// Neither metadata nor unapproved error details cross this boundary.
func FromGRPC(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return rpcerrors.RebuildError(err)
	}
	s, ok := status.FromError(err)
	if !ok {
		return connect.NewError(connect.CodeInternal, "plugin operation failed")
	}
	code, ok := grpcToConnect[s.Code()]
	if !ok {
		code = connect.CodeInternal
	}
	converted := connect.NewError(code, "")
	for index, detail := range s.Proto().Details {
		if index >= 8 {
			break
		}
		if detail == nil || detail.TypeUrl != "type.googleapis.com/google.rpc.ErrorInfo" || len(detail.Value) > 2048 {
			continue
		}
		converted = converted.WithDetail(&connect.ErrorDetail{Type: "google.rpc.ErrorInfo", Value: detail.Value})
	}
	return rpcerrors.RebuildError(converted)
}

// ToGRPC preserves approved codes while removing remote messages and details.
func ToGRPC(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	safe := rpcerrors.RebuildError(err)
	for code, mapped := range grpcToConnect {
		if mapped == connect.CodeOf(safe) {
			var ce *connect.Error
			errors.As(safe, &ce)
			out := status.New(code, ce.Message())
			if detail := version.SafeDetail(safe); detail != nil {
				var info errdetails.ErrorInfo
				if proto.Unmarshal(detail.Value, &info) == nil {
					if enriched, err := out.WithDetails(&info); err == nil {
						out = enriched
					}
				}
			}
			return out.Err()
		}
	}
	return status.Error(codes.Internal, "plugin operation failed")
}
