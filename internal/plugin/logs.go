package plugin

import (
	"context"
	"regexp"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/proto"
)

type logSubscription struct {
	queue    chan *pb.LogRecord
	overflow chan struct{}
	failed   bool
}

var logCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

// RecordLog accepts reason codes rather than arbitrary worker output. User
// tokens, filenames, login URLs and command output do not enter the log ring.
func (m *Supervisor) RecordLog(id string, record *pb.LogRecord) error {
	if record == nil || !logCode.MatchString(record.Message) || len(record.Fields) > 0 {
		return connect.NewError(connect.CodeInvalidArgument, "plugin logs require a reason code without arbitrary fields")
	}
	switch record.Level {
	case "debug", "info", "warn", "error":
	default:
		return connect.NewError(connect.CodeInvalidArgument, "invalid log level")
	}
	m.record(id, &pb.LogRecord{Timestamp: time.Now().Unix(), Level: record.Level, Message: record.Message})
	return nil
}
func (m *Supervisor) record(id string, record *pb.LogRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	slot := m.slots[id]
	if slot == nil {
		return
	}
	slot.logs = append(slot.logs, record)
	if len(slot.logs) > 1000 {
		copy(slot.logs, slot.logs[len(slot.logs)-1000:])
		slot.logs = slot.logs[:1000]
	}
	for subscriber := range slot.subscribers {
		if subscriber.failed {
			continue
		}
		select {
		case subscriber.queue <- record:
		default:
			subscriber.failed = true
			close(subscriber.overflow)
		}
	}
}
func (m *Supervisor) WatchLogs(ctx context.Context, request *pb.PluginLogsRequest, send func(*pb.LogRecord) error) error {
	if request == nil || request.Tail > 1000 {
		return connect.NewError(connect.CodeInvalidArgument, "log tail must be at most 1000")
	}
	m.mu.Lock()
	slot := m.slots[request.PluginId]
	if slot == nil {
		m.mu.Unlock()
		return connect.NewError(connect.CodeNotFound, "unknown plugin")
	}
	count := int(request.Tail)
	if count > len(slot.logs) {
		count = len(slot.logs)
	}
	history := append([]*pb.LogRecord(nil), slot.logs[len(slot.logs)-count:]...)
	subscriber := &logSubscription{queue: make(chan *pb.LogRecord, 128), overflow: make(chan struct{})}
	if request.Follow {
		if slot.subscribers == nil {
			slot.subscribers = map[*logSubscription]bool{}
		}
		slot.subscribers[subscriber] = true
	}
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(slot.subscribers, subscriber); m.mu.Unlock() }()
	for _, record := range history {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := send(proto.Clone(record).(*pb.LogRecord)); err != nil {
			return err
		}
	}
	if !request.Follow {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.ctx.Done():
			return connect.NewError(connect.CodeUnavailable, "plugin supervisor stopped")
		case <-subscriber.overflow:
			return connect.NewError(connect.CodeResourceExhausted, "slow log subscriber; fetch latest tail")
		case record := <-subscriber.queue:
			if err := send(proto.Clone(record).(*pb.LogRecord)); err != nil {
				return err
			}
		}
	}
}
