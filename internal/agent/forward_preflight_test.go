package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

// Hold only the return from an already completed real CheckPort RPC. Both its
// final response and underlying Close have succeeded before the barrier opens;
// replacing the pool cannot retroactively cancel that received response. This
// exposes the schedule between successful preflight and the local transaction.
type completedForwardCheck struct {
	connect.ClientStream
	entered  chan struct{}
	release  <-chan struct{}
	once     sync.Once
	err      error
	received bool
}

func (s *completedForwardCheck) Receive(message any) error {
	err := s.ClientStream.Receive(message)
	s.received = err == nil
	return err
}

func (s *completedForwardCheck) Close() error {
	s.once.Do(func() {
		s.err = s.ClientStream.Close()
		if s.err == nil && s.received {
			close(s.entered)
			<-s.release
		}
	})
	return s.err
}

func TestForwardPreflightMustMatchCommittedServerGeneration(t *testing.T) {
	for _, mutation := range []string{"unchanged", "configuration", "credential", "cancel"} {
		t.Run(mutation, func(t *testing.T) {
			s, r, record := forwardResources(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if _, err := r.Resolve(ctx, record.Config.Id); err != nil {
				t.Fatal(err)
			}
			entered, released := make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(released) })
			defer release()
			var barrierOnce sync.Once
			r.mu.Lock()
			pool := r.pools[record.Config.Id]
			pool.nextCheck = time.Now().Add(time.Hour)
			original := pool.client
			pool.client = connect.NewClient(clientTransport{original}, func(next connect.ClientFunc) connect.ClientFunc {
				return func(call context.Context, spec connect.Spec) (connect.ClientStream, error) {
					stream, err := next(call, spec)
					if err != nil || spec.Procedure != rpc.ShikiDaemonCheckPortProcedure {
						return stream, err
					}
					var wrapped connect.ClientStream = stream
					barrierOnce.Do(func() { wrapped = &completedForwardCheck{ClientStream: stream, entered: entered, release: released} })
					return wrapped, nil
				}
			})
			r.mu.Unlock()
			request := createRequest(record.Config.Id)
			result := make(chan error, 1)
			go func() { _, err := s.CreateForward(ctx, request); result <- err }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("successful real CheckPort did not reach completion barrier", ctx.Err())
			}
			current := findServer(t, s, record.Config.Id)
			switch mutation {
			case "configuration":
				updated := proto.Clone(current.Config).(*apb.ServerConfig)
				updated.Name = "changed-after-check"
				if _, err := s.UpsertServer(ctx, &apb.UpsertServerRequest{Config: updated, ExpectedRevision: current.Revision, RequestId: "change-after-check"}); err != nil {
					t.Fatal(err)
				}
			case "credential":
				// The token bytes still authenticate on the same test server, but
				// the committed credential generation must invalidate old checks.
				if _, err := s.SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: current.Config.Id, Token: []byte("shiki_forward_test"), ExpectedCredentialRevision: current.CredentialRevision, RequestId: "credential-after-check"}); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			}
			release()
			var err error
			select {
			case err = <-result:
			case <-time.After(time.Second):
				t.Fatal("forward creation did not leave completion barrier")
			}
			if mutation == "unchanged" {
				if err != nil {
					t.Fatal("unchanged generation rejected", err)
				}
			} else if mutation == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("canceled preflight committed desired state", err)
				}
			} else if connect.CodeOf(err) != connect.CodeAborted {
				t.Fatal("obsolete successful preflight committed against a replacement server generation", err)
			}
			snapshot, err := r.store.View()
			if err != nil {
				t.Fatal(err)
			}
			if mutation != "unchanged" {
				if len(snapshot.Buckets["forwards"]) != 0 {
					t.Fatal("rejected preflight left desired forward state")
				}
				if _, exists := snapshot.Requests["CreateForward\x00"+request.RequestId]; exists {
					t.Fatal("rejected preflight left a pending/completed request")
				}
				if mutation != "cancel" {
					if _, err := s.CreateForward(ctx, request); err != nil {
						t.Fatal("fresh preflight could not retry the uncommitted request ID", err)
					}
				}
			}
			if mutation != "cancel" {
				list, err := s.ListForwards(ctx, &pb.Empty{})
				if err != nil || len(list.Forwards) != 1 {
					t.Fatal("successful fresh operation did not produce exactly one forward", list, err)
				}
			}
		})
	}
}
