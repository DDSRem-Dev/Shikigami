package ui

import (
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"strings"
	"testing"
	"time"
)

func TestDashboardShowsCleanupDeadlineWithoutTerminalInjection(t *testing.T) {
	warning, due := time.Now().Unix(), time.Now().Add(time.Hour).Unix()
	model := &Model{width: 80, height: 40, sessions: []*pb.Session{{Id: "sess_cleanup", CleanupStatus: "warned", CleanupWarningAt: &warning, CleanupAfter: &due}}}
	view := model.View()
	if !strings.Contains(view, "Cleanup after "+time.Unix(due, 0).UTC().Format(time.RFC3339)) {
		t.Fatal("deadline not visible at 80 columns", view)
	}
}

func TestDashboardShowsUnselectedCleanupWarning(t *testing.T) {
	model := &Model{width: 80, height: 24, streamOpen: true, sessions: []*pb.Session{{Id: "selected"}, {Id: "other", CleanupStatus: "warned"}}}
	model.panels[0].rows = []string{"selected session", "other session"}
	if view := model.View(); !strings.Contains(view, "[cleanup warning] other session") {
		t.Fatal("unselected warning hidden", view)
	}
}
