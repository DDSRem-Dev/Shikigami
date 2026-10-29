package terminalstate_test

import (
	"fmt"
	"os"
	"os/exec"
)

func tmuxSize(f *terminalFixture) (string, error) {
	cmd := exec.CommandContext(f.ctx, "tmux", "-L", fmt.Sprintf("shiki-%d", os.Getuid()), "display-message", "-p", "-t", f.pane.PaneID, "#{pane_height} #{pane_width}")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func paneCursor(f *terminalFixture) string {
	cmd := exec.CommandContext(f.ctx, "tmux", "-L", fmt.Sprintf("shiki-%d", os.Getuid()), "display-message", "-p", "-t", f.pane.PaneID, "#{cursor_x},#{cursor_y},alternate=#{alternate_on}")
	out, _ := cmd.CombinedOutput()
	return string(out)
}
