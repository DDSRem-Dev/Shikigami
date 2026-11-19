package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type sessionCreateFailureProbe struct {
	managementProbe
	selectionError error
}

func (p *sessionCreateFailureProbe) ListServers(ctx context.Context, request *pb.Empty) (*apb.ServerList, error) {
	if p.selectionError != nil {
		return nil, p.selectionError
	}
	return p.managementProbe.ListServers(ctx, request)
}

type sessionCreateFailureRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	mu             sync.Mutex
	selectionError error
	createError    error
	requests       []*pb.CreateSessionRequest
	listCalls      int
	completeReplay bool
}

func (r *sessionCreateFailureRemote) GetProject(_ context.Context, request *pb.GetProjectRequest) (*pb.Project, error) {
	if r.selectionError != nil {
		return nil, r.selectionError
	}
	return &pb.Project{Id: request.Id, Name: "fixture"}, nil
}

func (r *sessionCreateFailureRemote) CreateSession(_ context.Context, request *pb.CreateSessionRequest) (*pb.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, proto.Clone(request).(*pb.CreateSessionRequest))
	if r.completeReplay && len(r.requests) == 2 && proto.Equal(r.requests[0], request) {
		return &pb.Session{Id: "sess_retained", ProjectId: request.ProjectId, Name: request.Name}, nil
	}
	return nil, r.createError
}

func (r *sessionCreateFailureRemote) ListSessions(context.Context, *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	return &pb.ListSessionsResponse{}, nil
}

func runSessionCreateFailure(t *testing.T, path string, json bool, requestID string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := newRoot(nil, &stdout, &stderr)
	args := []string{"--config", path, "--server", "server", "--project", "proj_fixture", "sessions", "create", "fixture-session", "--no-claude", "--description", "private-description", "--tags", "private-tag"}
	if json {
		args = append(args, "--json")
	}
	if requestID != "" {
		args = append(args, "--request-id", requestID)
	}
	root.SetArgs(args)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := root.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}

func TestSessionCreateSelectionFailureDoesNotSendCreate(t *testing.T) {
	for _, stage := range []string{"server", "project"} {
		for _, failure := range []error{
			connect.NewError(connect.CodeDeadlineExceeded, "deadline exceeded"),
			connect.NewError(connect.CodeCanceled, "canceled"),
			connect.NewError(connect.CodeUnavailable, "REMOTE_RPC_FAILED"),
			version.MissingRemoteCapability("session.v1"),
		} {
			t.Run(stage+"/"+connect.CodeOf(failure).String(), func(t *testing.T) {
				remote := &sessionCreateFailureRemote{}
				probe := &sessionCreateFailureProbe{}
				if stage == "server" {
					probe.selectionError = failure
				} else {
					remote.selectionError = failure
				}
				path := managementFixtureHandlers(t, probe, remote)
				out, stderr, err := runSessionCreateFailure(t, path, true, "")
				if err == nil || !strings.Contains(err.Error(), "resolve session creation server/project; CreateSession request has not been sent") || connect.CodeOf(err) != connect.CodeOf(failure) {
					t.Fatalf("selection stage or error code lost: %v", err)
				}
				if out != "" || stderr != "" || strings.Contains(err.Error(), "result may be unknown") || strings.Contains(err.Error(), "--request-id") {
					t.Fatalf("selection error changed JSON stdout or claimed a submitted request: %q %q %v", out, stderr, err)
				}
				remote.mu.Lock()
				defer remote.mu.Unlock()
				if len(remote.requests) != 0 || remote.listCalls != 0 {
					t.Fatalf("selection failure triggered mutation or recovery read: %+v", remote.requests)
				}
				if version.SafeDetail(failure) != nil && version.Describe(err, "fixture") != version.Describe(failure, "fixture") {
					t.Fatal("safe protocol diagnostic was lost")
				}
			})
		}
	}
}

func TestSessionCreateUncertainResultProvidesExactIDWithoutReplay(t *testing.T) {
	for _, json := range []bool{false, true} {
		for _, code := range []connect.Code{connect.CodeCanceled, connect.CodeDeadlineExceeded, connect.CodeUnavailable} {
			for _, explicitID := range []string{"", "user-stable-request"} {
				t.Run(fmt.Sprintf("json=%t/%s/explicit=%t", json, code, explicitID != ""), func(t *testing.T) {
					remote := &sessionCreateFailureRemote{createError: connect.NewError(code, "REMOTE_RPC_FAILED"), completeReplay: true}
					path := managementFixtureHandlers(t, &managementProbe{}, remote)
					out, stderr, err := runSessionCreateFailure(t, path, json, explicitID)
					if err == nil || connect.CodeOf(err) != code || out != "" || stderr != "" {
						t.Fatalf("failure code/output contract changed: %q %q %v", out, stderr, err)
					}
					remote.mu.Lock()
					if len(remote.requests) != 1 || remote.listCalls != 0 {
						t.Fatalf("uncertain mutation was retried or followed by an unrequested read: %d %d", len(remote.requests), remote.listCalls)
					}
					id := remote.requests[0].RequestId
					remote.mu.Unlock()
					if id == "" || explicitID != "" && id != explicitID {
						t.Fatal("request ID not preserved", id)
					}
					for _, message := range []string{"result may be unknown", "first run shiki sessions list", "same --server and --project", "Absence from the list does not prove failure", "While the result remains unknown", "keep all original arguments", "--request-id", fmt.Sprintf("%q", id), "retrieve the original outcome", "does not rerun a recorded failure", "Only after confirming the original request failed, explicitly create with a new request ID"} {
						if !strings.Contains(err.Error(), message) {
							t.Fatalf("missing actionable recovery %q: %v", message, err)
						}
					}
					if strings.Contains(err.Error(), "private-description") || strings.Contains(err.Error(), "private-tag") {
						t.Fatal("creation arguments leaked into error")
					}
					// A deliberate second invocation can use the displayed ID to
					// recover the retained resource with exactly the original request.
					out, stderr, err = runSessionCreateFailure(t, path, true, id)
					result := new(pb.Session)
					if err != nil || stderr != "" || protojson.Unmarshal([]byte(out), result) != nil || result.Id != "sess_retained" {
						t.Fatalf("explicit deduplicated retry did not retain the success JSON contract: %q %v", out, err)
					}
					remote.mu.Lock()
					defer remote.mu.Unlock()
					if len(remote.requests) != 2 || !proto.Equal(remote.requests[0], remote.requests[1]) {
						t.Fatal("retry changed the original mutation")
					}
				})
			}
		}
	}
}

func TestSessionCreateDefiniteFailureKeepsCodeAndSafeDetails(t *testing.T) {
	for _, failure := range []error{
		connect.NewError(connect.CodePermissionDenied, "permission denied"),
		connect.NewError(connect.CodeResourceExhausted, "SESSION_LIMIT"),
		connect.NewError(connect.CodeInvalidArgument, "INVALID_SESSION_NAME"),
		version.MissingRemoteCapability("session.v1"),
	} {
		t.Run(connect.CodeOf(failure).String(), func(t *testing.T) {
			remote := &sessionCreateFailureRemote{createError: failure}
			path := managementFixtureHandlers(t, &managementProbe{}, remote)
			out, stderr, err := runSessionCreateFailure(t, path, true, "")
			if err == nil || connect.CodeOf(err) != connect.CodeOf(failure) || !strings.HasPrefix(err.Error(), "CreateSession failed: ") || out != "" || stderr != "" {
				t.Fatalf("definite failure contract changed: %q %v", out, err)
			}
			if strings.Contains(err.Error(), "unknown") || strings.Contains(err.Error(), "--request-id") {
				t.Fatal("business rejection was labeled as an uncertain transport result", err)
			}
			if version.SafeDetail(failure) != nil && version.Describe(err, "fixture") != version.Describe(failure, "fixture") {
				t.Fatal("safe diagnostic detail was replaced")
			}
		})
	}
}

func TestSessionCreateErrorPreservesLocalCancellationAndEscapesRequestID(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		wrapped := sessionCreateError(cause, "request\n\x1b[31m")
		if !errors.Is(wrapped, cause) || strings.ContainsAny(wrapped.Error(), "\n\x1b") || !strings.Contains(wrapped.Error(), `"request\n\x1b[31m"`) {
			t.Fatalf("local cause or safe request ID rendering lost: %v", wrapped)
		}
	}
}
