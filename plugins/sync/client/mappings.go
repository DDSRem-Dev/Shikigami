package client

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"time"

	"connectrpc.com/connect/v2"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

const archivedMappingsBucket = "sync_archived_mappings"

func retryDelay(attempt int) time.Duration {
	base := min(time.Second*time.Duration(1<<min(attempt, 5)), 30*time.Second)
	return min(time.Duration(float64(base)*(0.8+rand.Float64()*0.4)), 30*time.Second)
}

// Subscribe before reading the snapshot. Events are only invalidation hints;
// a failed read never means a mapping disappeared.
func (w *Worker) watchMappings() {
	defer close(w.mappingsDone)
	defer func() {
		w.mu.Lock()
		if w.lifetime.Err() == nil {
			w.mappingFeedFailed = true
		}
		w.mu.Unlock()
	}()
	for failures := 0; w.lifetime.Err() == nil; {
		err := w.mappingSubscription()
		if w.lifetime.Err() != nil {
			return
		}
		if !errors.Is(err, io.EOF) && connect.CodeOf(err) != connect.CodeUnavailable && connect.CodeOf(err) != connect.CodeResourceExhausted && connect.CodeOf(err) != connect.CodeDeadlineExceeded {
			return
		}
		if failures >= 5 {
			return
		}
		timer := time.NewTimer(retryDelay(failures))
		failures++
		select {
		case <-w.lifetime.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (w *Worker) mappingSubscription() error {
	stream, err := w.core.SubscribeEvents(w.lifetime, &ppb.SubscribeRequest{Scope: &ppb.Scope{}, EventTypes: []string{"mapping", "server"}})
	if err != nil {
		return err
	}
	defer stream.Close()
	for {
		event, err := stream.Receive()
		if err != nil {
			return err
		}
		if event == nil || !event.GetRescanRequired() {
			continue
		}
		if err = w.refreshMappings(w.lifetime, event); err != nil {
			return err
		}
	}
}
func (w *Worker) refreshMappings(parent context.Context, event *ppb.Event) error {
	w.configure.Lock()
	defer w.configure.Unlock()
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return context.Canceled
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	list, err := w.core.ListMappings(ctx, &ppb.Scope{})
	if err != nil {
		return err
	}
	if list == nil || len(list.Mappings) > 128 {
		return fail(connect.CodeResourceExhausted, "MAPPING_LIST_INVALID")
	}
	current := map[string]bool{}
	for _, m := range list.Mappings {
		if m == nil || !validID(m.Id, "map_") || current[m.Id] {
			return fail(connect.CodeDataLoss, "MAPPING_LIST_INVALID")
		}
		current[m.Id] = true
	}
	snapshot, err := w.store.View()
	if err != nil {
		return err
	}
	for id := range snapshot.Buckets[mappingsBucket] {
		if current[id] {
			continue
		}
		if err = w.archiveMapping(id); err != nil {
			return err
		}
	}
	for _, m := range list.Mappings {
		// A changed host record will fail the ConfigureMapping authoritative check;
		// its next invalidation supplies the new complete configuration.
		if _, err = w.configureMapping(parent, m); err != nil {
			// One changed or unavailable root must not terminate observation of
			// the other mappings or turn this mapping into a disappearance.
			if parent.Err() != nil {
				return parent.Err()
			}
			continue
		}
		if event.Type == "server" {
			w.mu.Lock()
			task := w.tasks[m.Id]
			w.mu.Unlock()
			if task != nil && (event.GetScope().GetServerId() == "" || event.GetScope().GetServerId() == m.ServerId) {
				task.pause()
				task.op.Lock()
				task.resetRetry()
				if task.data.Mapping.SyncEnabled {
					task.start()
				}
				task.op.Unlock()
			}
		}
	}
	w.notify()
	return nil
}
func (w *Worker) archiveMapping(id string) error {
	w.mu.Lock()
	task := w.tasks[id]
	w.mu.Unlock()
	if task != nil {
		task.pause()
		task.op.Lock()
		defer task.op.Unlock()
	}
	if err := w.update(func(snapshot *storage.Snapshot) error {
		var r record
		if err := snapshot.Get(mappingsBucket, id, &r); err != nil {
			return err
		}
		r.State = "archived"
		r.Reason = "MAPPING_REMOVED"
		if err := snapshot.Put(archivedMappingsBucket, id, &r); err != nil {
			return err
		}
		delete(snapshot.Buckets[mappingsBucket], id)
		return nil
	}); err != nil {
		return err
	}
	if task != nil {
		if err := errors.Join(task.local.Close(), task.stageDir.Close()); err != nil {
			return err
		}
		w.mu.Lock()
		delete(w.tasks, id)
		w.mu.Unlock()
	}
	return nil
}
