package agent

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

// PublishEvent checks Err after reading the mapping. This barrier holds that
// already validated event while a real resource mutation completes.
type mappingEventBarrier struct {
	context.Context
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type mappingConfigureReply struct {
	sdk.SyncClient
	configure func(context.Context, *ppb.Mapping) (*ppb.SyncState, error)
}

func (w mappingConfigureReply) ConfigureMapping(ctx context.Context, mapping *ppb.Mapping) (*ppb.SyncState, error) {
	return w.configure(ctx, mapping)
}

func TestMappingConfigureReplyCannotOutliveMappingMutation(t *testing.T) {
	for _, mutation := range []string{"remove", "revision"} {
		t.Run(mutation, func(t *testing.T) {
			s, r, mapping := mappingEventResources(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			entered, released := make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(released) })
			defer release()
			worker := mappingConfigureReply{configure: func(ctx context.Context, got *ppb.Mapping) (*ppb.SyncState, error) {
				// A synchronous CoreAPI callback must complete before the response
				// barrier, proving no host lock is held across ConfigureMapping.
				if _, err := s.ListMappings(ctx, &apb.ListMappingsRequest{}); err != nil {
					return nil, err
				}
				close(entered)
				select {
				case <-released:
					return &ppb.SyncState{MappingId: got.Id, MappingRevision: got.Revision, State: "paused"}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
			result := make(chan error, 1)
			go func() { result <- r.configureMapping(ctx, worker, mapping) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("worker callback blocked on host lock", ctx.Err())
			}
			if mutation == "remove" {
				if _, err := s.RemoveMapping(ctx, &apb.RemoveMappingRequest{MappingId: mapping.Id, ExpectedRevision: mapping.Revision, ArchivePending: true, RequestId: "remove"}); err != nil {
					t.Fatal(err)
				}
			} else {
				current, err := s.PutMapping(ctx, &apb.PutMappingRequest{Mapping: mapping, ExpectedRevision: mapping.Revision, RequestId: "revise"})
				if err != nil {
					t.Fatal(err)
				}
				core := &pluginCore{resources: r, id: "sync"}
				if _, err := core.PublishEvent(ctx, mappingSyncEvent(s, current)); err != nil {
					t.Fatal(err)
				}
			}
			release()
			select {
			case err := <-result:
				if err != nil {
					t.Fatal("superseded configuration response should be ignored", err)
				}
			case <-ctx.Done():
				t.Fatal("configure did not finish", ctx.Err())
			}
			cached, present := r.syncStates.Load(mapping.Id)
			if mutation == "remove" && present {
				t.Error("late ConfigureMapping response recreated orphan cache")
			}
			if mutation == "revision" && (!present || cached.(*ppb.SyncState).MappingRevision == mapping.Revision) {
				t.Error("late ConfigureMapping response overwrote current revision")
			}
		})
	}
}

func TestMappingConfigureReplyValidatesAndOwnsState(t *testing.T) {
	_, r, mapping := mappingEventResources(t)
	valid := &ppb.SyncState{MappingId: mapping.Id, MappingRevision: mapping.Revision, State: "paused"}
	for _, scenario := range []struct {
		name  string
		state *ppb.SyncState
	}{
		{"nil", nil},
		{"wrong-mapping", &ppb.SyncState{MappingId: "map_other", MappingRevision: mapping.Revision, State: "paused"}},
		{"wrong-revision", &ppb.SyncState{MappingId: mapping.Id, MappingRevision: "rev_other", State: "paused"}},
		{"invalid-state", &ppb.SyncState{MappingId: mapping.Id, MappingRevision: mapping.Revision, State: "invented"}},
		{"valid", valid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			worker := mappingConfigureReply{configure: func(context.Context, *ppb.Mapping) (*ppb.SyncState, error) { return scenario.state, nil }}
			err := r.configureMapping(context.Background(), worker, mapping)
			if scenario.name != "valid" {
				if connect.CodeOf(err) != connect.CodeDataLoss {
					t.Fatal("malformed response accepted", err)
				}
				if _, exists := r.syncStates.Load(mapping.Id); exists {
					t.Fatal("malformed response entered cache")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			valid.PendingFiles++
			cached, exists := r.syncStates.Load(mapping.Id)
			if !exists || cached.(*ppb.SyncState).PendingFiles != 0 {
				t.Fatal("cache did not own an isolated response snapshot")
			}
		})
	}
}

func TestMappingCanceledConfigureReplyCannotPublish(t *testing.T) {
	_, r, mapping := mappingEventResources(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := mappingConfigureReply{configure: func(context.Context, *ppb.Mapping) (*ppb.SyncState, error) {
		cancel()
		return &ppb.SyncState{MappingId: mapping.Id, MappingRevision: mapping.Revision, State: "paused"}, nil
	}}
	if err := r.configureMapping(ctx, worker, mapping); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled call accepted a worker response", err)
	}
	if _, exists := r.syncStates.Load(mapping.Id); exists {
		t.Fatal("canceled worker response entered cache")
	}
}

func (c *mappingEventBarrier) Err() error {
	c.once.Do(func() {
		close(c.entered)
		select {
		case <-c.release:
		case <-c.Context.Done():
		}
	})
	return c.Context.Err()
}

func mappingEventResources(t *testing.T) (*Service, *Resources, *ppb.Mapping) {
	t.Helper()
	s, r, server := forwardResources(t)
	mapping, err := s.PutMapping(context.Background(), &apb.PutMappingRequest{
		RequestId: "mapping", Mapping: &ppb.Mapping{ServerId: server.Config.Id, ProjectId: "proj_mapping", LocalRoot: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, r, mapping
}

func mappingSyncEvent(s *Service, mapping *ppb.Mapping) *ppb.PublishEventRequest {
	return &ppb.PublishEventRequest{ExpectedHostInstanceId: s.Registry.InstanceID(), Event: &ppb.Event{
		EventId: "mapping-event", Type: "sync",
		Scope:   &ppb.Scope{ServerId: mapping.ServerId, ProjectId: mapping.ProjectId, MappingId: mapping.Id},
		Payload: &ppb.Event_SyncState{SyncState: &ppb.SyncState{MappingId: mapping.Id, MappingRevision: mapping.Revision, State: "paused"}},
	}}
}

func TestMappingEventCannotOutliveMappingMutation(t *testing.T) {
	for _, mutation := range []string{"remove", "revision"} {
		t.Run(mutation, func(t *testing.T) {
			s, r, mapping := mappingEventResources(t)
			core := &pluginCore{resources: r, id: "sync"}
			request := mappingSyncEvent(s, mapping)
			if _, err := core.PublishEvent(context.Background(), request); err != nil {
				t.Fatal("current event rejected", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			barrier := &mappingEventBarrier{Context: ctx, entered: make(chan struct{}), release: make(chan struct{})}
			release := sync.OnceFunc(func() { close(barrier.release) })
			defer release()
			result := make(chan error, 1)
			go func() { _, err := core.PublishEvent(barrier, request); result <- err }()
			select {
			case <-barrier.entered:
			case <-ctx.Done():
				t.Fatal("event did not reach post-validation barrier", ctx.Err())
			}
			sub, unsubscribe := s.Events.subscribe()
			defer unsubscribe()
			<-sub.queue // Initial rescan hint.
			wantCode := connect.CodeNotFound
			if mutation == "remove" {
				if _, err := s.RemoveMapping(ctx, &apb.RemoveMappingRequest{MappingId: mapping.Id, ExpectedRevision: mapping.Revision, ArchivePending: true, RequestId: "remove"}); err != nil {
					t.Fatal("remove blocked by event or failed", err)
				}
				snapshot, err := r.store.View()
				if err != nil {
					t.Fatal(err)
				}
				if err := snapshot.Get("mappings", mapping.Id, new(ppb.Mapping)); !errors.Is(err, os.ErrNotExist) || snapshot.Buckets["archived_mappings"][mapping.Id] == nil {
					t.Fatal("removal did not archive and delete desired mapping", err)
				}
				if _, present := r.syncStates.Load(mapping.Id); present {
					t.Fatal("removal retained cached state before late event")
				}
			} else {
				changed := proto.Clone(mapping).(*ppb.Mapping)
				changed.SessionHints = map[string]string{"subdir": "changed"}
				current, err := s.PutMapping(ctx, &apb.PutMappingRequest{Mapping: changed, ExpectedRevision: mapping.Revision, RequestId: "revise"})
				if err != nil {
					t.Fatal("revision mutation blocked by event or failed", err)
				}
				if _, err := core.PublishEvent(ctx, mappingSyncEvent(s, current)); err != nil {
					t.Fatal("new revision event rejected", err)
				}
				wantCode = connect.CodeAborted
			}
			// The mutation and its current-state hint have finished. A stale
			// publication must neither overwrite the cache nor send another hint.
			for len(sub.queue) > 0 {
				<-sub.queue
			}
			release()
			select {
			case err := <-result:
				if connect.CodeOf(err) != wantCode {
					t.Errorf("late event accepted after %s: %v", mutation, err)
				}
			case <-ctx.Done():
				t.Fatal("late event did not finish", ctx.Err())
			}
			cached, present := r.syncStates.Load(mapping.Id)
			if mutation == "remove" && present {
				t.Error("late event recreated orphan mapping cache")
			}
			if mutation == "revision" && (!present || cached.(*ppb.SyncState).MappingRevision == mapping.Revision) {
				t.Error("late event overwrote current mapping revision")
			}
			for len(sub.queue) > 0 {
				if event := <-sub.queue; event.Type == "sync" && event.ResourceId == mapping.Id {
					t.Error("late event published stale sync hint")
				}
			}
		})
	}
}
