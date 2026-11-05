//go:build darwin || linux

package main

import (
	"context"
	"os"
	"testing"
	"time"
)

// smartPTYProcess owns the terminal until the command has relinquished it,
// including when an assertion fails while the command is still running.
type smartPTYProcess struct {
	t        *testing.T
	master   *os.File
	slave    *os.File
	cancel   context.CancelFunc
	finished chan struct{}
}

func (p *smartPTYProcess) start(cancel context.CancelFunc, run func() error) <-chan error {
	p.cancel = cancel
	p.finished = make(chan struct{})
	done := make(chan error, 1)
	go func() {
		defer close(p.finished)
		done <- run()
	}()
	return done
}

func (p *smartPTYProcess) close() {
	if p.cancel != nil {
		p.cancel()
	}
	// Closing only the master wakes blocked output without racing the command's
	// access to the slave's descriptor during input and terminal restoration.
	_ = p.master.Close()
	if p.finished != nil {
		select {
		case <-p.finished:
		case <-time.After(5 * time.Second):
			go func() {
				<-p.finished
				_ = p.slave.Close()
			}()
			p.t.Error("command did not terminate during PTY cleanup")
			return
		}
	}
	_ = p.slave.Close()
}
