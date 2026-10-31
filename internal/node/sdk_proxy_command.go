package node

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"time"
)

const maxSystemProxyOutput = 64 << 10

type proxyCommandOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func (b *proxyCommandOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.overflow || len(p) > maxSystemProxyOutput-b.buffer.Len() {
		b.overflow = true
		b.cancel()
		return 0, errSystemProxyRead
	}
	return b.buffer.Write(p)
}

func readProxyCommand(parent context.Context, executable string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	output := &proxyCommandOutput{cancel: cancel}
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdout = output
	command.Stderr = output
	command.WaitDelay = 100 * time.Millisecond
	if err := command.Run(); err != nil || ctx.Err() != nil {
		return nil, errSystemProxyRead
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.overflow {
		return nil, errSystemProxyRead
	}
	return append([]byte(nil), output.buffer.Bytes()...), nil
}
