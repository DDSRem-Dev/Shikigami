package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

const (
	ProjectsBucket  = "projects"
	SessionsBucket  = "sessions"
	SnapshotsBucket = "snapshots"
)

func ValidResourceID(id, prefix string) bool { return pb.ValidResourceID(id, prefix) }

func ValidateName(name string) error {
	if name == "" || len(name) > 128 || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return errors.New("name must contain 1–128 bytes without surrounding whitespace")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("name cannot contain control characters")
		}
	}
	return nil
}

// Within uses path components, never a textual prefix. This checks stored path
// shape only; live filesystem access must separately use root-confined handles.
func Within(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ValidateDaemon supplies Store.Options.Validate for a daemon resource store.
// All three resource collections and idempotency results commit together.
// Runtime state must be reconstructed after opening the store, never restored
// from persisted attachment IDs or PIDs.
func ValidateDaemon(user string) func(*Snapshot) error {
	return func(state *Snapshot) error {
		projects := map[string]*pb.Project{}
		names := map[string]bool{}
		for id, data := range state.Buckets[ProjectsBucket] {
			p := new(pb.Project)
			if err := strictResource(data, p); err != nil {
				return fmt.Errorf("project %s: %w", id, err)
			}
			if p.Id != id || !ValidResourceID(id, "proj") || p.User != user || p.CreatedAt <= 0 || !filepath.IsAbs(p.Path) || filepath.Clean(p.Path) != p.Path {
				return errors.New("invalid persistent project")
			}
			if err := ValidateName(p.Name); err != nil {
				return err
			}
			if names[p.Name] {
				return errors.New("duplicate project name")
			}
			names[p.Name] = true
			if len(p.SessionIds) > 0 {
				return errors.New("project session IDs must be derived, not stored")
			}
			for _, other := range projects {
				if Within(other.Path, p.Path) || Within(p.Path, other.Path) {
					return errors.New("overlapping project roots")
				}
			}
			projects[id] = p
		}
		names = map[string]bool{}
		for id, data := range state.Buckets[SessionsBucket] {
			s := new(pb.Session)
			if err := strictResource(data, s); err != nil {
				return fmt.Errorf("session %s: %w", id, err)
			}
			p := projects[s.ProjectId]
			if s.Id != id || !ValidResourceID(id, "sess") || p == nil || s.CreatedAt <= 0 || !filepath.IsAbs(s.WorkingDir) || filepath.Clean(s.WorkingDir) != s.WorkingDir || !Within(p.Path, s.WorkingDir) {
				return errors.New("session references invalid project or directory")
			}
			if err := ValidateName(s.Name); err != nil {
				return err
			}
			key := s.ProjectId + "\x00" + s.Name
			if names[key] {
				return errors.New("duplicate session name within project")
			}
			names[key] = true
			switch s.Lifecycle {
			case "creating", "ready", "deleting", "failed":
			default:
				return errors.New("invalid session lifecycle")
			}
			if s.CleanupStatus != "" || s.CleanupWarningAt != nil || s.CleanupAfter != nil || s.ClientCount != 0 || s.IsConnected || s.ClaudePid != 0 || s.ControllerAttachmentId != "" || s.ControlRevision != "" || s.CpuUsage != 0 || s.MemoryUsage != 0 {
				return errors.New("runtime session fields cannot be persisted")
			}
			if s.TmuxSession != "" && s.TmuxSession != "shiki-"+id {
				return errors.New("tmux session identity mismatch")
			}
		}
		for id, data := range state.Buckets[SnapshotsBucket] {
			s := new(pb.Snapshot)
			if err := strictResource(data, s); err != nil {
				return fmt.Errorf("snapshot %s: %w", id, err)
			}
			if s.Id != id || !ValidResourceID(id, "snap") || projects[s.ProjectId] == nil || !ValidResourceID(s.SessionId, "sess") || s.SchemaVersion != 1 || s.CreatedAt <= 0 {
				return errors.New("invalid configuration snapshot")
			}
			if s.WorkingDir != "" && s.WorkingDir != "." {
				if path.IsAbs(s.WorkingDir) || path.Clean(s.WorkingDir) != s.WorkingDir || s.WorkingDir == ".." || strings.HasPrefix(s.WorkingDir, "../") || strings.ContainsAny(s.WorkingDir, "\\\x00") {
					return errors.New("snapshot directory must be project-relative")
				}
			}
		}
		return nil
	}
}

func strictResource(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}
