package tmux

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

type captureReader struct{ io.Reader }

func TestScreenCaptureBoundIncludesCopyFastPaths(t *testing.T) {
	payload := bytes.Repeat([]byte("s"), MaxScreenBytes+1)
	output := &boundedCapture{limit: MaxScreenBytes}
	n, err := io.Copy(output, captureReader{bytes.NewReader(payload)})
	if err != nil || n != int64(len(payload)) || output.Len() != MaxScreenBytes || !output.truncated {
		t.Fatalf("capture copy: consumed=%d retained=%d truncated=%t err=%v", n, output.Len(), output.truncated, err)
	}
}

func TestScreenCaptureDrainsAndBoundsChildOutput(t *testing.T) {
	if os.Getenv("SHIKI_CAPTURE_OUTPUT_FIXTURE") == "1" {
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("s"), MaxScreenBytes+1))
		_, _ = os.Stderr.Write(bytes.Repeat([]byte("e"), 4097))
		os.Exit(0)
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIKI_CAPTURE_OUTPUT_FIXTURE", "1")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, "-test.run=^TestScreenCaptureDrainsAndBoundsChildOutput$")
	cmd.WaitDelay = time.Second
	stdout, stderr := &boundedCapture{limit: MaxScreenBytes}, &boundedCapture{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != MaxScreenBytes || stderr.Len() != 4096 || !stdout.truncated || !stderr.truncated {
		t.Fatalf("capture bounds: stdout=%d/%t stderr=%d/%t", stdout.Len(), stdout.truncated, stderr.Len(), stderr.truncated)
	}
}
