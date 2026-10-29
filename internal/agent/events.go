package agent

import (
	"context"
	"sync"

	"connectrpc.com/connect/v2"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
)

const EventQueueSize = 256

type eventSubscriber struct {
	queue    chan *agentpb.AgentEvent
	overflow chan struct{}
	failed   bool
}

// Events is a bounded state-hint bus, not a replay log. Callers publish only
// non-secret resource identifiers; consumers query current snapshots.
type Events struct {
	mu          sync.Mutex
	instance    string
	sequence    uint64
	subscribers map[*eventSubscriber]struct{}
}

func NewEvents(instance string) *Events {
	return &Events{instance: instance, subscribers: map[*eventSubscriber]struct{}{}}
}
func (e *Events) Publish(event *agentpb.AgentEvent) {
	if event == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sequence++
	event = proto.Clone(event).(*agentpb.AgentEvent)
	event.InstanceId = e.instance
	event.Sequence = e.sequence
	for s := range e.subscribers {
		if s.failed {
			continue
		}
		select {
		case s.queue <- proto.Clone(event).(*agentpb.AgentEvent):
		default:
			s.failed = true
			close(s.overflow)
		}
	}
}
func (e *Events) subscribe() (*eventSubscriber, func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := &eventSubscriber{queue: make(chan *agentpb.AgentEvent, EventQueueSize), overflow: make(chan struct{})}
	e.sequence++
	s.queue <- &agentpb.AgentEvent{InstanceId: e.instance, Sequence: e.sequence, Type: "rescan_required"}
	e.subscribers[s] = struct{}{}
	return s, func() { e.mu.Lock(); delete(e.subscribers, s); e.mu.Unlock() }
}
func (s *eventSubscriber) receive(ctx context.Context) (*agentpb.AgentEvent, error) {
	select {
	case <-s.overflow:
		return nil, connect.NewError(connect.CodeResourceExhausted, "EVENT_QUEUE_OVERFLOW")
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.overflow:
		return nil, connect.NewError(connect.CodeResourceExhausted, "EVENT_QUEUE_OVERFLOW")
	case event := <-s.queue:
		return event, nil
	}
}
