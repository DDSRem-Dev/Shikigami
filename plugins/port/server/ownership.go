package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"connectrpc.com/connect/v2"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/protobuf/proto"
)

var errForeignProcess = errors.New("foreign process")

type processIdentity struct {
	pid, parent int32
	created     int64
}

func inspectProcess(ctx context.Context, pid int32) (processIdentity, error) {
	p, e := process.NewProcessWithContext(ctx, pid)
	if e != nil {
		return processIdentity{}, e
	}
	uids, e := p.UidsWithContext(ctx)
	if e != nil || len(uids) == 0 {
		return processIdentity{}, fmt.Errorf("process owner unavailable")
	}
	for _, uid := range uids {
		if uid != uint32(os.Getuid()) {
			return processIdentity{}, errForeignProcess
		}
	}
	created, e := p.CreateTimeWithContext(ctx)
	if e != nil || created <= 0 {
		return processIdentity{}, fmt.Errorf("process identity unavailable")
	}
	parent, e := p.PpidWithContext(ctx)
	if e != nil {
		return processIdentity{}, e
	}
	return processIdentity{pid, parent, created}, nil
}
func sessionRoots(list *pb.ListSessionsResponse) map[int32]*pb.Session {
	roots := map[int32]*pb.Session{}
	for _, s := range list.GetSessions() {
		if s.ClaudePid > 0 && s.Id != "" && s.ProjectId != "" && s.TmuxPane != "" && s.TmuxWindow != "" {
			if _, found := roots[s.ClaudePid]; found {
				roots[s.ClaudePid] = nil
			} else {
				roots[s.ClaudePid] = s
			}
		}
	}
	return roots
}

// The host supplies freshly verified pane roots, never persisted PID claims.
// OS ancestry/UID/start-time checks remain inside the port plugin.
func (w *Worker) scanOwned(ctx context.Context) ([]*pb.OpenPort, error) {
	ports, e := w.scanner.Scan(ctx)
	if e != nil {
		return nil, e
	}
	hasPID := false
	for i, p := range ports {
		ports[i] = proto.Clone(p).(*pb.OpenPort)
		ports[i].SessionId = ""
		ports[i].SourceIdentity = ""
		hasPID = hasPID || p.Pid > 0
	}
	if !hasPID {
		return ports, nil
	}
	sessions, e := w.core.ListSessions(ctx, &ppb.Scope{})
	if e != nil {
		return nil, e
	}
	roots := sessionRoots(sessions)
	observed := map[int32]processIdentity{}
	used := map[int32]*pb.Session{}
	for _, port := range ports {
		chain := []processIdentity{}
		seen := map[int32]bool{}
		pid := port.Pid
		var root *pb.Session
		for pid > 0 && len(chain) < 128 && !seen[pid] {
			seen[pid] = true
			identity, ok := observed[pid]
			if !ok {
				var err error
				identity, err = inspectProcess(ctx, pid)
				if err != nil {
					if !errors.Is(err, errForeignProcess) {
						return nil, err
					}
					break
				}
				if len(observed) >= 4096 {
					return nil, fmt.Errorf("ownership process limit")
				}
				observed[pid] = identity
			}
			chain = append(chain, identity)
			if candidate, ok := roots[pid]; ok {
				root = candidate
				break
			}
			pid = identity.parent
		}
		if root == nil {
			continue
		}
		stable := true
		for _, before := range chain {
			after, err := inspectProcess(ctx, before.pid)
			if err != nil {
				return nil, err
			}
			if after != before {
				stable = false
				break
			}
		}
		if !stable {
			continue
		}
		used[root.ClaudePid] = root
		hash := sha256.New()
		fmt.Fprintf(hash, "%s/%s/%s/%s", root.Id, root.ProjectId, root.TmuxWindow, root.TmuxPane)
		for _, identity := range chain {
			fmt.Fprintf(hash, "/%d:%d:%d", identity.pid, identity.parent, identity.created)
		}
		port.SessionId = root.Id
		port.SourceIdentity = hex.EncodeToString(hash.Sum(nil))
	}
	after, e := w.core.ListSessions(ctx, &ppb.Scope{})
	if e != nil {
		return nil, e
	}
	current := sessionRoots(after)
	for pid, before := range used {
		now := current[pid]
		if now == nil || now.Id != before.Id || now.ProjectId != before.ProjectId || now.TmuxPane != before.TmuxPane || now.TmuxWindow != before.TmuxWindow {
			for _, p := range ports {
				if p.SessionId == before.Id {
					p.SessionId = ""
					p.SourceIdentity = ""
				}
			}
		}
	}
	return ports, nil
}
func targetAddress(address string) string {
	switch address {
	case "0.0.0.0":
		return "127.0.0.1"
	case "::":
		return "::1"
	}
	return address
}
func (w *Worker) CheckPort(parent context.Context, r *pb.CheckPortRequest) (*pb.CheckPortResponse, error) {
	ctx, done, e := w.begin(parent, metadataOperation)
	if e != nil {
		return nil, e
	}
	defer done()
	return w.checkPort(ctx, r)
}
func (w *Worker) checkPort(ctx context.Context, r *pb.CheckPortRequest) (*pb.CheckPortResponse, error) {
	if r == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "PORT_TARGET_REQUIRED")
	}
	if e := w.config.Authorize(r.RemoteAddress, r.Port); e != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, "PORT_TARGET_NOT_ALLOWED")
	}
	if !r.Automatic {
		if r.SourceIdentity != "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, "MANUAL_SOURCE_IDENTITY")
		}
		if r.SessionId != "" {
			s, e := w.core.GetSession(ctx, &ppb.Scope{SessionId: r.SessionId})
			if e != nil {
				return nil, e
			}
			if s.GetId() != r.SessionId {
				return nil, connect.NewError(connect.CodePermissionDenied, "PORT_SESSION_UNAVAILABLE")
			}
		}
		return &pb.CheckPortResponse{Port: &pb.OpenPort{Port: r.Port, BindAddress: r.RemoteAddress, Protocol: "tcp", SessionId: r.SessionId}}, nil
	}
	if r.SessionId == "" || r.SourceIdentity == "" || len(r.SourceIdentity) != 64 || portProtected(r.Port) {
		return nil, connect.NewError(connect.CodePermissionDenied, "AUTOMATIC_PORT_DENIED")
	}
	ports, e := w.scanOwned(ctx)
	if e != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "PORT_SCAN_FAILED")
	}
	var match *pb.OpenPort
	for _, p := range ports {
		if targetAddress(p.BindAddress) == r.RemoteAddress && p.Port == r.Port {
			if match != nil {
				return nil, connect.NewError(connect.CodePermissionDenied, "AMBIGUOUS_PORT_OWNER")
			}
			match = p
		}
	}
	if match == nil || match.SessionId != r.SessionId || match.SourceIdentity != r.SourceIdentity {
		return nil, connect.NewError(connect.CodePermissionDenied, "PORT_OWNER_CHANGED")
	}
	return &pb.CheckPortResponse{Port: match}, nil
}
func portProtected(port int32) bool { return schema.Protected(uint16(port)) }
