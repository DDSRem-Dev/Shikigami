package node

import (
	"strings"

	"tailscale.com/health"
	"tailscale.com/ipn"
	"tailscale.com/tsconst"
)

// The pinned SDK forwards RegisterResponse.Error as ErrMessage, but HTTP
// registration errors only appear in login-state health. Neither channel is a
// typed auth-key error: only explicit key rejection text is terminal. The raw
// text can contain credentials and must never escape this classifier.
func authKeyRejected(notification ipn.Notify) bool {
	if notification.ErrMessage != nil && explicitKeyRejection(*notification.ErrMessage) {
		return true
	}
	if notification.Health != nil {
		warning := notification.Health.Warnings[tsconst.HealthWarnableLoginState]
		return explicitKeyRejection(warning.Args[health.ArgError])
	}
	return false
}

func explicitKeyRejection(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if strings.HasPrefix(message, "register request: http ") {
		status, body, ok := strings.Cut(strings.TrimPrefix(message, "register request: http "), ": ")
		if !ok || (status != "400" && status != "401" && status != "403") {
			return false
		}
		message = strings.TrimSpace(body)
	}
	for _, prefix := range []string{"invalid authkey", "invalid auth key", "invalid key"} {
		if message == prefix || strings.HasPrefix(message, prefix+":") {
			return true
		}
	}
	return false
}
