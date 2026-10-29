package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/reconcile"
	"google.golang.org/protobuf/proto"
)

const ledgerSchema = 2

type fileOperation struct {
	ID          string           `json:"id"`
	CommitState string           `json:"commit_state"`
	Phase       string           `json:"phase"`
	SourcePath  string           `json:"source_path"`
	TargetPath  string           `json:"target_path"`
	SourceSide  string           `json:"source_side"`
	TargetSide  string           `json:"target_side"`
	Source      *ppb.FileVersion `json:"source"`
	Target      *ppb.FileVersion `json:"target"`
	Result      *ppb.FileVersion `json:"result,omitempty"`
	Stage       string           `json:"stage,omitempty"`
	Size        int64            `json:"size"`
}

func ledgerOptions() storage.Options {
	return storage.Options{SchemaVersion: ledgerSchema, DisableRecovery: true, Validate: validateState, Migrations: map[int]func(*storage.Snapshot) error{1: func(s *storage.Snapshot) error {
		// Validate the old records before changing them; missing fields are not a
		// license to turn damaged state into an empty mapping or baseline.
		if err := validateState(s); err != nil {
			return err
		}
		for _, bucket := range []string{mappingsBucket, archivedMappingsBucket} {
			for key := range s.Buckets[bucket] {
				var r record
				if err := s.Get(bucket, key, &r); err != nil {
					return err
				}
				if len(r.Operations) != 0 || len(r.LegacyUnverified) != 0 {
					return errors.New("schema one contains an unrecognized file journal")
				}
				r.Operations = map[string]*fileOperation{}
				r.LegacyUnverified = map[string]bool{}
				for name := range r.Baselines {
					r.LegacyUnverified[name] = true
				}
				for name := range r.Conflicts {
					r.LegacyUnverified[name] = true
				}
				for name := range r.Copies {
					r.LegacyUnverified[name] = true
				}
				if err := s.Put(bucket, key, &r); err != nil {
					return err
				}
			}
		}
		s.SchemaVersion = ledgerSchema
		return nil
	}}}
}

func validateOperation(name string, op *fileOperation, r *record) error {
	if op == nil || !validID(op.ID, "fileop_") || op.TargetPath != name || !validPath(name) || !validPath(op.SourcePath) || op.Size < 0 || op.Size > schema.MaxAutoSyncSize {
		return errors.New("invalid file operation identity")
	}
	if op.SourceSide != "local" && op.SourceSide != "remote" || op.TargetSide != "local" && op.TargetSide != "remote" || op.SourceSide == "local" && op.TargetSide == "local" {
		return errors.New("invalid file operation direction")
	}
	if op.SourcePath != name {
		if op.SourceSide != "remote" || r.Copies[name] != op.SourcePath {
			return errors.New("untracked conflict copy operation")
		}
	} else if op.SourceSide == op.TargetSide {
		return errors.New("same-side file operation")
	}
	for _, v := range []*ppb.FileVersion{op.Source, op.Target} {
		if _, err := reconcile.Decide(v, v, v); err != nil {
			return err
		}
	}
	if op.Source.Exists {
		if !validStage(op.Stage) {
			return errors.New("invalid file operation stage")
		}
	} else if op.Stage != "" || op.Size != 0 || op.CommitState != "possible" {
		return errors.New("deletion operation contains staged bytes")
	}
	if op.CommitState != "incomplete" && op.CommitState != "possible" {
		return errors.New("unknown operation commit possibility")
	}
	switch op.Phase {
	case "prepared":
		if op.Result != nil {
			return errors.New("prepared operation contains a commit result")
		}
	case "committed":
		if op.CommitState != "possible" {
			return errors.New("committed operation never released complete input")
		}
		if _, err := reconcile.Decide(op.Result, op.Result, op.Result); err != nil {
			return err
		}
		if !reconcile.Same(op.Source, op.Result) {
			return errors.New("file operation commit differs from source")
		}
	default:
		return errors.New("unknown file operation phase")
	}
	return nil
}
func validStage(name string) bool {
	return strings.HasPrefix(name, "transfer-") && len(name) > len("transfer-") && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00")
}
func (t *task) side(files sdk.FileService) string {
	if files == sdk.FileService(t.local) {
		return "local"
	}
	return "remote"
}

// Prepared intent and its verified source snapshot are durable before mutation.
// The pending-byte budget excludes the operation being explicitly superseded;
// one bounded source snapshot may coexist with it while preparing replacement.
func (t *task) prepare(source, destination sdk.FileService, sourcePath, targetPath string, observed, target *ppb.FileVersion, stage string, size int64) error {
	var pendingBytes int64
	for name, op := range t.data.Operations {
		if name != targetPath {
			pendingBytes += op.Size
		}
	}
	if pendingBytes+size > max(64<<20, t.worker.maxFileBytes) {
		return fail(connect.CodeResourceExhausted, "PENDING_TRANSFER_STORAGE_LIMIT")
	}
	if t.data.Operations == nil {
		t.data.Operations = map[string]*fileOperation{}
	}
	commitState := "incomplete"
	if !observed.Exists {
		commitState = "possible"
	}
	t.data.Operations[targetPath] = &fileOperation{ID: id("fileop_"), Phase: "prepared", CommitState: commitState, SourcePath: sourcePath, TargetPath: targetPath, SourceSide: t.side(source), TargetSide: t.side(destination), Source: proto.Clone(observed).(*ppb.FileVersion), Target: proto.Clone(target).(*ppb.FileVersion), Stage: stage, Size: size}
	return t.persist()
}

// Mark before releasing the last payload chunk (or an empty file header).
// Some stream adapters half-close after a Send error without requesting EOF;
// incomplete byte count, not EOF delivery, is the safe no-commit proof.
func (t *task) allowCommit(targetPath string) error {
	op := t.data.Operations[targetPath]
	if op == nil || op.Phase != "prepared" {
		return errors.New("missing prepared file operation")
	}
	if op.CommitState == "possible" {
		return nil
	}
	op.CommitState = "possible"
	return t.persist()
}

func (t *task) committed(targetPath string, result *ppb.FileVersion) error {
	op := t.data.Operations[targetPath]
	if op == nil || op.Phase != "prepared" {
		return errors.New("missing prepared file operation")
	}
	op.Phase = "committed"
	op.Result = proto.Clone(result).(*ppb.FileVersion)
	return t.persist()
}
func (t *task) referencesStage(name string) bool {
	for _, op := range t.data.Operations {
		if op.Stage == name {
			return true
		}
	}
	return false
}

// An uncertain operation never falls back to an ordinary old-baseline decision:
// a user may have reverted either side to that baseline after the commit.
func (t *task) recoverOperation(ctx context.Context, name string, allowRetry bool) error {
	project := t.data.Mapping.ProjectId
	local, _, err := observation(ctx, t.local, project, name, t.worker.maxFileBytes)
	if err != nil {
		return err
	}
	remote, _, err := observation(ctx, t.remote, project, name, t.worker.maxFileBytes)
	if err != nil {
		return err
	}
	if !reconcile.Same(local, remote) {
		op := t.data.Operations[name]
		if allowRetry && op.Phase == "prepared" && op.CommitState == "incomplete" {
			source, destination := sdk.FileService(t.local), t.remote
			observed, target := local, remote
			if op.SourceSide == "remote" {
				source = t.remote
				observed = remote
			}
			if op.TargetSide == "local" {
				destination = t.local
				target = local
			}
			if op.SourcePath != name {
				observed, _, err = observation(ctx, source, project, op.SourcePath, t.worker.maxFileBytes)
				if err != nil {
					return err
				}
			}
			if proto.Equal(observed, op.Source) && proto.Equal(target, op.Target) {
				if err = t.transfer(ctx, source, destination, op.SourcePath, name, observed, target); err != nil {
					return err
				}
				if err = t.establish(ctx, name); err != nil {
					return err
				}
				return t.persist()
			}
		}
		return t.conflict(name, local, remote)
	}
	t.acceptCommonBaseline(name, remote)
	return t.persist()
}

func verifyStages(ctx context.Context, directory *stagingDirectory, operations map[string]*fileOperation) error {
	for _, op := range operations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if op.Stage == "" {
			continue
		}
		if !validStage(op.Stage) {
			return errors.New("invalid retained file stage")
		}
		f, err := directory.Open(op.Stage)
		if err != nil {
			return fmt.Errorf("open retained stage: %w", err)
		}
		err = verifyStage(ctx, f, op)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func verifyStage(ctx context.Context, f *os.File, op *fileOperation) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != op.Size {
		return errors.New("retained stage metadata changed")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("retained stage ownership changed")
	}
	sum := sha256.New()
	n, err := io.CopyBuffer(sum, io.LimitReader(cancelableStageReader{ctx: ctx, Reader: f}, op.Size+1), make([]byte, 256<<10))
	if err != nil {
		return err
	}
	if n != op.Size || hex.EncodeToString(sum.Sum(nil)) != op.Source.ContentHash {
		return errors.New("retained stage content changed")
	}
	return nil
}
func fileVersion(info *pb.FileInfo) *ppb.FileVersion {
	return &ppb.FileVersion{Exists: true, ContentHash: info.ContentHash, Mode: info.Mode, Revision: info.Revision}
}

type cancelableStageReader struct {
	ctx context.Context
	io.Reader
}

func (r cancelableStageReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// A legacy active snapshot does not prove its last mutation never committed.
// Preserve its known paths behind review until fresh equality or a successful
// explicit resolution establishes a new common baseline.
func (t *task) recoverLegacy(ctx context.Context, name string) error {
	project := t.data.Mapping.ProjectId
	local, _, err := observation(ctx, t.local, project, name, t.worker.maxFileBytes)
	if err != nil {
		return err
	}
	remote, _, err := observation(ctx, t.remote, project, name, t.worker.maxFileBytes)
	if err != nil {
		return err
	}
	if !reconcile.Same(local, remote) {
		return t.conflict(name, local, remote)
	}
	t.acceptCommonBaseline(name, remote)
	return t.persist()
}
func (t *task) acceptCommonBaseline(name string, remote *ppb.FileVersion) {
	t.data.Baselines[name] = proto.Clone(remote).(*ppb.FileVersion)
	delete(t.data.Conflicts, name)
	delete(t.data.Operations, name)
	delete(t.data.LegacyUnverified, name)
}
