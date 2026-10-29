package server

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// Hiding WriterTo exercises io.Copy's destination ReaderFrom dispatch. An
// embedded bytes.Buffer used to bypass boundedOutput.Write on this path.
type outputReader struct{ io.Reader }

func TestScannerOutputBoundIncludesCopyFastPaths(t *testing.T) {
	for _, limit := range []int{16 << 10, 1 << 20} {
		output := &boundedOutput{limit: limit}
		_, err := io.Copy(output, outputReader{bytes.NewReader(bytes.Repeat([]byte("x"), limit+1))})
		if err == nil || output.Len() > limit {
			t.Fatalf("limit %d: len=%d error=%v", limit, output.Len(), err)
		}
	}
}

func TestScannerRejectsOversizedChildStdout(t *testing.T) {
	if os.Getenv("SHIKI_SCANNER_OUTPUT_FIXTURE") == "1" {
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), (1<<20)+1))
		os.Exit(0)
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIKI_SCANNER_OUTPUT_FIXTURE", "1")
	data, err := command(t.Context(), program, "-test.run=^TestScannerRejectsOversizedChildStdout$")
	if err == nil || len(data) > 1<<20 {
		t.Fatalf("oversized child output: bytes=%d error=%v", len(data), err)
	}
}
