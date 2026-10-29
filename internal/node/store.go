package node

import (
	"errors"
	"os"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"tailscale.com/ipn"
)

// stateStore uses the project's fail-closed transactional store. The SDK's
// default FileStore treats an empty file as a fresh identity, which is unsafe
// for unattended recovery. Auth keys are never supplied to this store.
type stateStore struct{ store *storage.Store }
type stateValue struct {
	Value []byte `json:"value"`
}

func (s *stateStore) ReadState(id ipn.StateKey) ([]byte, error) {
	snapshot, err := s.store.View()
	if err != nil {
		return nil, err
	}
	var result stateValue
	err = snapshot.Get("tailscale", string(id), &result)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ipn.ErrStateNotExist
	}
	return result.Value, err
}
func (s *stateStore) WriteState(id ipn.StateKey, data []byte) error {
	return s.store.Update(func(snapshot *storage.Snapshot) error {
		if data == nil {
			delete(snapshot.Buckets["tailscale"], string(id))
			return nil
		}
		return snapshot.Put("tailscale", string(id), stateValue{Value: data})
	})
}

type loginRecord struct {
	ID          string   `json:"id"`
	RequestID   string   `json:"request_id"`
	Fingerprint [32]byte `json:"fingerprint"`
	Interactive bool     `json:"interactive"`
	Finished    bool     `json:"finished"`
	State       string   `json:"state"`
	Reason      string   `json:"reason,omitempty"`
	Addresses   []string `json:"addresses,omitempty"`
}

func (n *Node) saveLoginLocked(attempt *loginAttempt, status *agentpb.NodeStatus, finished bool) error {
	record := loginRecord{ID: attempt.id, RequestID: attempt.requestID, Fingerprint: attempt.fingerprint, Interactive: attempt.interactive, Finished: finished, State: status.State, Reason: status.ReasonCode, Addresses: append([]string(nil), status.Addresses...)}
	return n.state.Update(func(snapshot *storage.Snapshot) error { return snapshot.Put("login", attempt.requestID, record) })
}
func (n *Node) recoverLogins() error {
	snapshot, err := n.state.View()
	if err != nil {
		return err
	}
	if len(snapshot.Buckets["login"]) > 128 {
		return errors.New("node login history exceeds supported capacity")
	}
	for requestID := range snapshot.Buckets["login"] {
		var record loginRecord
		if err = snapshot.Get("login", requestID, &record); err != nil {
			return err
		}
		if record.ID == "" || record.RequestID != requestID {
			return errors.New("invalid node login history")
		}
		if !record.Finished {
			record.Finished = true
			record.State = "needs_login"
			record.Reason = "LOGIN_INTERRUPTED"
			record.Addresses = nil
			if err = n.state.Update(func(current *storage.Snapshot) error { return current.Put("login", requestID, record) }); err != nil {
				return err
			}
		}
		attempt := &loginAttempt{id: record.ID, requestID: record.RequestID, fingerprint: record.Fingerprint, interactive: record.Interactive, finished: true, done: make(chan struct{}), result: &agentpb.NodeStatus{State: record.State, ReasonCode: record.Reason, LoginId: record.ID, Addresses: record.Addresses}}
		close(attempt.done)
		n.requests[requestID] = attempt
	}
	return nil
}
