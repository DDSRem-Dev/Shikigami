package server

import (
	"context"
	"time"
)

func (worker *Worker) monitorRuntime(ctx context.Context) {
	defer worker.active.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = worker.observeRuntime(ctx)
		}
	}
}

// A runtime's PID/socket is never persisted as proof of life. Observe the
// verified unit and private identity, and mark a Chrome crash without launching
// anything. An uncertain observation remains unknown, never a guessed crash.
func (worker *Worker) observeRuntime(parent context.Context) error {
	worker.mu.Lock()
	if worker.closed || worker.db.Runtime == nil || worker.db.State == "stopped" || worker.db.State == "crashed" {
		worker.mu.Unlock()
		return nil
	}
	spec := *worker.db.Runtime
	worker.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	snapshot, err := worker.manager.Inspect(ctx, spec)
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.closed || worker.db.Runtime == nil || worker.db.Runtime.InstanceID != spec.InstanceID || worker.db.State == "stopped" {
		return nil
	}
	if err != nil {
		worker.observationError = "Runtime identity/state could not be observed; no restart or PID-based action was attempted."
		return err
	}
	worker.observationError = ""
	if worker.db.State == snapshot.State && worker.db.Reason == snapshot.Reason {
		return nil
	}
	worker.db.State, worker.db.Reason = snapshot.State, snapshot.Reason
	if snapshot.State != "running" {
		worker.detachAllLocked()
	}
	if err := worker.saveLocked(); err != nil {
		worker.observationError = "Observed runtime state could not be persisted; recheck status."
		return err
	}
	return nil
}
