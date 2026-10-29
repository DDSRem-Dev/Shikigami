// Package errors reconstructs safe application errors at RPC boundaries.
package errors

import (
	"connectrpc.com/connect/v2"
	"context"
	"errors"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"io"
	"net"
)

// RebuildError removes the remote marker, untrusted text, and all unapproved
// details at a proxy boundary. EOF denotes a completed stream, not an error.
func RebuildError(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	code := connect.CodeInternal
	if errors.Is(err, context.Canceled) {
		code = connect.CodeCanceled
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = connect.CodeDeadlineExceeded
	} else {
		var ce *connect.Error
		if errors.As(err, &ce) {
			code = ce.Code()
		} else {
			var ne net.Error
			if errors.As(err, &ne) {
				code = connect.CodeUnavailable
			}
		}
	}
	messages := map[connect.Code]string{
		connect.CodeCanceled: "operation canceled", connect.CodeUnknown: "downstream operation failed",
		connect.CodeInvalidArgument: "invalid request", connect.CodeDeadlineExceeded: "operation deadline exceeded",
		connect.CodeNotFound: "resource not found", connect.CodeAlreadyExists: "resource already exists",
		connect.CodePermissionDenied: "operation is not permitted", connect.CodeResourceExhausted: "resource limit exceeded",
		connect.CodeFailedPrecondition: "operation prerequisites are not satisfied", connect.CodeAborted: "resource changed; refresh and retry",
		connect.CodeOutOfRange: "value is outside the permitted range", connect.CodeUnimplemented: "operation is not supported",
		connect.CodeInternal: "internal operation failed", connect.CodeUnavailable: "service unavailable",
		connect.CodeDataLoss: "data integrity check failed", connect.CodeUnauthenticated: "authentication required",
	}
	message, ok := messages[code]
	if !ok {
		code = connect.CodeInternal
		message = messages[code]
	}
	return connect.NewError(code, message).WithDetail(version.SafeDetail(err)).WithCause(err)
}
