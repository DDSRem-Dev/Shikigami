package server

import (
	"context"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

// A native transport Send may remain blocked until its handler returns. Keep
// one bounded writer per download, so lifetime cancellation can return the
// handler instead of waiting on that same transport and deadlocking Stop.
func cancellableChunkSender(parent context.Context, send func(*epb.BrowserFileChunk) error) (func(*epb.BrowserFileChunk) error, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	queue := make(chan *epb.BrowserFileChunk)
	results := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-queue:
				err := send(frame)
				results <- err
				if err != nil {
					return
				}
			}
		}
	}()
	return func(frame *epb.BrowserFileChunk) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case queue <- frame:
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-results:
			return err
		}
	}, cancel
}
