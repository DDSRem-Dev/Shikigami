package client

import (
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"strings"
	"testing"
	"time"
)

func TestCleanupNoticeDoesNotInventMissingSupport(t *testing.T) {
	now := time.Unix(2000000, 0)
	warning, due := now.Unix(), now.Add(time.Hour).Unix()
	for _, s := range []*pb.Session{{}, {CleanupStatus: "future-unknown"}} {
		if CleanupNotice(s, now) != "" {
			t.Fatal("unknown field became cleanup permission")
		}
	}
	message := CleanupNotice(&pb.Session{CleanupStatus: "warned", CleanupWarningAt: &warning, CleanupAfter: &due}, now)
	if !strings.Contains(message, "Cleanup after") || !strings.Contains(message, "restart Claude") || !strings.Contains(message, "project files retained") {
		t.Fatal(message)
	}
	if got := CleanupNotice(&pb.Session{CleanupStatus: "live_pane"}, now); !strings.Contains(got, "shell") || !strings.Contains(got, "not scheduled") {
		t.Fatal(got)
	}
	if got := CleanupNotice(&pb.Session{CleanupStatus: "disabled", ClaudeStatus: "stopped", LastActive: 1}, now); !strings.Contains(got, "reviewing") || !strings.Contains(got, "Check for live tasks") {
		t.Fatal(got)
	}
}
