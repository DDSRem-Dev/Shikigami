//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type rotationTCPResult struct {
	At    time.Time
	Tail  []byte
	Error string
}
type rotationTCPObserver struct {
	earliest, latest time.Time
	done             chan struct{}
	result           rotationTCPResult
	once             sync.Once
	conn             net.Conn
}

func startRotationTCPObserver(conn net.Conn, earliest, latest time.Time) *rotationTCPObserver {
	o := &rotationTCPObserver{earliest: earliest, latest: latest, done: make(chan struct{}), conn: conn}
	_ = conn.SetReadDeadline(latest)
	go func() {
		defer close(o.done)
		tail, err := io.ReadAll(io.LimitReader(conn, int64(len(rotationTail)+1)))
		o.result = rotationTCPResult{At: time.Now(), Tail: tail}
		if err != nil {
			o.result.Error = err.Error()
		}
	}()
	return o
}
func (o *rotationTCPObserver) wait(ctx context.Context) rotationTCPResult {
	select {
	case <-o.done:
		return o.result
	case <-ctx.Done():
		o.close()
		return rotationTCPResult{At: time.Now(), Error: ctx.Err().Error()}
	}
}
func (o *rotationTCPObserver) close() { o.once.Do(func() { o.conn.Close(); <-o.done }) }
func rotationTCPTerminationAllowed(result rotationTCPResult, earliest, latest time.Time) error {
	if result.At.Before(earliest) {
		return errors.New("held TCP terminated before any port credential could expire")
	}
	if result.At.After(latest) {
		return errors.New("held TCP did not terminate within the port expiry bound")
	}
	if result.Error != "" {
		return fmt.Errorf("held TCP did not end cleanly: %s", result.Error)
	}
	if len(result.Tail) != 0 && !bytes.Equal(result.Tail, []byte(rotationTail)) {
		return errors.New("held TCP replayed application bytes")
	}
	return nil
}
