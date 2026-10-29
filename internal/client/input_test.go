package client

import (
	"bytes"
	"testing"
)

func TestInputDecoderFragmentedPaste(t *testing.T) {
	input := []byte("before\x1b[200~pasted\x11\x1b[201~after\x11discard")
	want := []byte("before\x1b[200~pasted\x11\x1b[201~after")
	for split := 0; split <= len(input); split++ {
		decoder := InputDecoder{Detach: 17}
		var got []byte
		detached := false
		for _, chunk := range [][]byte{input[:split], input[split:]} {
			output, stop := decoder.Feed(chunk)
			got = append(got, output...)
			if stop {
				detached = true
				break
			}
		}
		if !detached || !bytes.Equal(got, want) {
			t.Fatalf("split %d: %q detached=%v", split, got, detached)
		}
	}
	decoder := InputDecoder{Detach: 17}
	var got []byte
	for _, b := range input {
		output, stop := decoder.Feed([]byte{b})
		got = append(got, output...)
		if stop {
			break
		}
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("bytewise: %q", got)
	}
}
func TestInputDecoderDisabledAndImmediateEscape(t *testing.T) {
	for _, input := range [][]byte{[]byte("\x1b"), []byte("\x11\x1b[200~\x11\x1b[201~\x11")} {
		decoder := InputDecoder{}
		output, stop := decoder.Feed(input)
		if stop || !bytes.Equal(output, input) {
			t.Fatalf("%q, %v", output, stop)
		}
	}
}
