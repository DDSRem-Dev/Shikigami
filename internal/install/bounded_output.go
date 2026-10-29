package install

import (
	"bytes"
	"sync"
)

// cappedOutput drains child output without allowing unbounded memory growth.
// A truncated result is never treated as a complete machine-readable response.
type cappedOutput struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedOutput) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	size := len(data)
	remaining := max(c.limit-c.buffer.Len(), 0)
	if len(data) > remaining {
		data = data[:remaining]
		c.truncated = true
	}
	c.buffer.Write(data)
	return size, nil
}
func (c *cappedOutput) Bytes() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buffer.Bytes()...), c.truncated
}
