package client

import (
	"errors"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
)

// TerminalRetryable identifies interruptions for which a frontend may open a
// new attachment. It never authorizes replay of terminal input or auth fallback.
func TerminalRetryable(err error) bool {
	if connect.CodeOf(err) == connect.CodeUnauthenticated || connect.CodeOf(err) == connect.CodePermissionDenied {
		return false
	}
	if connection.IsRetryableNetworkError(err) || connect.CodeOf(err) == connect.CodeUnavailable {
		return true
	}
	var remote *connect.Error
	return errors.As(err, &remote) && remote.Code() == connect.CodeFailedPrecondition && (remote.Message() == "AGENT_RESTARTED" || remote.Message() == "HANDSHAKE_REQUIRED")
}
