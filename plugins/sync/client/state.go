package client

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"connectrpc.com/connect/v2"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/reconcile"
)

const maxFiles = 10000
const mappingsBucket = "sync_mappings"

type record struct {
	LegacyUnverified map[string]bool             `json:"legacy_unverified"`
	Operations       map[string]*fileOperation   `json:"operations"`
	Mapping          *ppb.Mapping                `json:"mapping"`
	Baselines        map[string]*ppb.FileVersion `json:"baselines"`
	Conflicts        map[string]*ppb.Conflict    `json:"conflicts"`
	Copies           map[string]string           `json:"copies"`
	State            string                      `json:"state"`
	Reason           string                      `json:"reason,omitempty"`
}

func fail(code connect.Code, reason string) error { return connect.NewError(code, reason) }
func id(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(raw[:])
}
func validID(s, prefix string) bool {
	if !strings.HasPrefix(s, prefix) || len(s) > 128 || len(s) == len(prefix) {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func validPath(path string) bool {
	return fs.ValidPath(path) && path != "." && !strings.Contains(path, "\\")
}
func validateState(snapshot *storage.Snapshot) error {
	for _, bucket := range []string{mappingsBucket, archivedMappingsBucket} {
		for key := range snapshot.Buckets[bucket] {
			var r record
			if snapshot.Get(bucket, key, &r) != nil || r.Mapping == nil || r.Mapping.Id != key || !validID(key, "map_") || !validID(r.Mapping.ServerId, "srv_") || !pb.ValidResourceID(r.Mapping.ProjectId, "proj") || !filepath.IsAbs(r.Mapping.LocalRoot) || r.Baselines == nil || r.Conflicts == nil || r.Copies == nil {
				return errors.New("invalid sync state")
			}
			if len(r.Baselines) > maxFiles || len(r.Conflicts) > maxFiles || len(r.Operations) > maxFiles || len(r.Copies) > maxFiles || len(r.LegacyUnverified) > maxFiles {
				return errors.New("sync state capacity exceeded")
			}
			if snapshot.SchemaVersion >= ledgerSchema && (r.Operations == nil || r.LegacyUnverified == nil) {
				return errors.New("file operation journal missing")
			}
			for name, pending := range r.LegacyUnverified {
				if !validPath(name) || !pending {
					return errors.New("invalid legacy recovery path")
				}
			}
			stages := map[string]bool{}
			var pendingBytes int64
			for name, op := range r.Operations {
				if err := validateOperation(name, op, &r); err != nil {
					return err
				}
				pendingBytes += op.Size
				if pendingBytes > max(64<<20, schema.MaxAutoSyncSize) {
					return errors.New("pending source snapshot budget exceeded")
				}
				if op.Stage != "" {
					if stages[op.Stage] {
						return errors.New("file stage referenced twice")
					}
					stages[op.Stage] = true
				}
			}
			for path, base := range r.Baselines {
				if !validPath(path) {
					return errors.New("invalid baseline path")
				}
				if _, err := reconcile.Decide(base, base, base); err != nil {
					return err
				}
			}
			for path, c := range r.Conflicts {
				if !validPath(path) || c == nil || c.Path != path || c.MappingId != key || !validID(c.Id, "conf_") {
					return errors.New("invalid conflict")
				}
				generation, err := reconcile.Generation(key, path, c.Base, c.Local, c.Remote)
				if err != nil || generation != c.Generation {
					return errors.New("invalid conflict generation")
				}
			}
			for copyPath, original := range r.Copies {
				if !validPath(copyPath) || !validPath(original) {
					return errors.New("invalid conflict copy")
				}
			}
		}
	}
	return nil
}
func (r *record) status() *ppb.SyncState {
	pending := make(map[string]bool, len(r.Conflicts)+len(r.Operations)+len(r.LegacyUnverified))
	for name := range r.Conflicts {
		pending[name] = true
	}
	for name := range r.Operations {
		pending[name] = true
	}
	for name := range r.LegacyUnverified {
		pending[name] = true
	}
	return &ppb.SyncState{MappingId: r.Mapping.Id, MappingRevision: r.Mapping.Revision, State: r.State, ReasonCode: r.Reason, ConflictCount: uint64(len(r.Conflicts)), PendingFiles: uint64(len(pending))}
}
