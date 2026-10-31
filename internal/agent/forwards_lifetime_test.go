package agent

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/types/known/structpb"
)

// ListForwards reads desired state before creating the worker-call deadline.
// Pause that boundary while StopForward actually commits and removes its cache.
type forwardListBarrier struct {
	context.Context
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *forwardListBarrier) Deadline() (time.Time, bool) {
	c.once.Do(func() {
		close(c.entered)
		select {
		case <-c.release:
		case <-c.Context.Done():
		}
	})
	return c.Context.Deadline()
}

func TestForwardListCannotResurrectRemovedCache(t *testing.T) {
	s, r, server := forwardResources(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	state, err := s.CreateForward(ctx, createRequest(server.Config.Id))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := host.NewSupervisor(ctx, host.SupervisorOptions{
		StateDir: filepath.Join(t.TempDir(), "plugin-intent"),
		Config: func(string, *structpb.Struct) (host.Config, error) {
			return host.Config{}, errors.New("unused stopped plugin")
		},
		Validate: func(string, *structpb.Struct) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.plugins.Store(nil)
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	s.plugins.Store(manager)
	barrier := &forwardListBarrier{Context: ctx, entered: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(barrier.release) })
	defer release()
	result := make(chan error, 1)
	go func() { _, err := s.ListForwards(barrier, &pb.Empty{}); result <- err }()
	select {
	case <-barrier.entered:
	case <-ctx.Done():
		t.Fatal("list did not reach worker-call boundary", ctx.Err())
	}
	if _, err := s.StopForward(ctx, &apb.StopForwardRequest{ForwardId: state.Spec.Id, ExpectedRevision: state.Spec.Revision, RequestId: "stop-during-list"}); err != nil {
		t.Fatal(err)
	}
	if _, exists := r.forwardStates.Load(state.Spec.Id); exists {
		t.Fatal("stop retained cached state")
	}
	release()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("list did not finish", ctx.Err())
	}
	if _, exists := r.forwardStates.Load(state.Spec.Id); exists {
		t.Fatal("late ListForwards recreated orphan cache after StopForward")
	}
}
