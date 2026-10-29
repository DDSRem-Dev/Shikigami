package client

import (
	"fmt"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"time"
)

// CleanupNotice renders only the finite server contract. It never turns a
// missing/unknown field into permission to delete, or injects terminal bytes.
func CleanupNotice(session *pb.Session, now time.Time) string {
	if session == nil {
		return ""
	}
	switch session.CleanupStatus {
	case "warned":
		if session.CleanupWarningAt == nil || session.CleanupAfter == nil || session.GetCleanupAfter() <= session.GetCleanupWarningAt() {
			return "Cleanup status unavailable"
		}
		return fmt.Sprintf("Cleanup after %s; project files retained. Reattach or restart Claude to cancel.", time.Unix(session.GetCleanupAfter(), 0).UTC().Format(time.RFC3339))
	case "live_pane":
		return "A pane is still running; automatic cleanup is not scheduled. Stop Claude leaves a shell; exit that shell before cleanup can be considered."
	case "attached":
		return "Terminal is attached; automatic cleanup is not scheduled."
	case "observing":
		return "Explicitly stopped with a dead pane; observing the idle window before publishing a cleanup warning."
	case "disabled":
		if session.ClaudeStatus == "stopped" && !session.IsConnected && session.LastActive > 0 && now.Unix()-session.LastActive >= int64((168*time.Hour)/time.Second) {
			return "Consider reviewing this stopped session for manual cleanup; automatic cleanup is disabled. Check for live tasks first; project files are retained."
		}
	case "unknown":
		return "Cleanup eligibility is unknown; automatic cleanup is not scheduled."
	case "", "not_explicitly_stopped":
		return ""
	}
	return ""
}
