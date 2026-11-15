package helper

import (
	"bytes"
	"io"
)

// Client invokes only the immutable installed binary through exact sudoers
// action arguments. No package-directory binary is ever executed as root.
type Client struct{}

type limitedBuffer struct {
	bytes.Buffer
	maximum int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.maximum-b.Len() {
		return 0, io.ErrShortBuffer
	}
	return b.Buffer.Write(data)
}
