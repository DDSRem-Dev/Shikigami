package server

import (
	"context"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
)

type operationClass uint8

const (
	metadataOperation operationClass = iota
	transferOperation
	watchOperation
)

var operationLimits = [...]int{limits.FileMetadataCalls, limits.FileTransfers, limits.FileWatches}

func (w *Worker) begin(parent context.Context, class operationClass) (context.Context, func(), error) {
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	w.mu.Lock()
	if w.closed || w.lifetime == nil || w.lifetime.Err() != nil {
		w.mu.Unlock()
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "SYNC_NOT_RUNNING")
	}
	if w.operations[class] >= operationLimits[class] {
		w.mu.Unlock()
		return nil, nil, connect.NewError(connect.CodeResourceExhausted, "FILE_OPERATION_LIMIT")
	}
	w.operations[class]++
	w.active.Add(1)
	lifetime := w.lifetime
	w.mu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(lifetime, cancel)
	return ctx, func() {
		stop()
		cancel()
		w.mu.Lock()
		w.operations[class]--
		w.mu.Unlock()
		w.active.Done()
	}, nil
}
