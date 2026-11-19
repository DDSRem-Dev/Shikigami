package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

type cancelSecondCopyRead struct {
	sdk.FileService
	cancel context.CancelFunc
	reads  int
}

func (p *cancelSecondCopyRead) ReadFile(ctx context.Context, in *pb.ReadFileRequest, send func(*pb.FileData) error) error {
	p.reads++
	if p.reads == 2 {
		if p.cancel != nil {
			p.cancel()
		}
		<-ctx.Done()
		return ctx.Err()
	}
	return p.FileService.ReadFile(ctx, in, send)
}

func partialCopyFixture(t *testing.T, interruption string) (*journalFixture, *ppb.ResolveConflictRequest, string) {
	t.Helper()
	f := newJournalFixture(t)
	write(t, f.task.data.Mapping.LocalRoot, "file.txt", "local conflict")
	write(t, f.remoteRoot, "file.txt", "remote conflict")
	if err := f.task.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	c := f.saved(t).Conflicts["file.txt"]
	request := &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "interrupted-copy"}
	ctx, cancel := context.WithCancel(t.Context())
	if interruption == "parent-deadline" {
		cancel()
		ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
	}
	defer cancel()
	switch interruption {
	case "parent-canceled":
		f.task.remote = &cancelSecondCopyRead{FileService: f.probe, cancel: cancel}
	case "parent-deadline":
		f.task.remote = &cancelSecondCopyRead{FileService: f.probe}
	case "remote-ack-lost-then-deleted":
		f.probe.lostACK = true
	default:
		f.probe.failReadAt = 2
	}
	_, err := f.task.worker.ResolveConflict(ctx, request)
	if interruption == "parent-canceled" {
		if !errors.Is(err, context.Canceled) {
			t.Fatal("copy ignored parent cancellation", err)
		}
	} else if interruption == "parent-deadline" {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("copy ignored parent deadline", err)
		}
	} else if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal("copy interruption was not reached", err)
	}
	r := f.saved(t)
	if len(r.Copies) != 1 || len(r.Operations) != 1 {
		t.Fatal("interruption lost the unique copy intent or retained operation")
	}
	var copyPath string
	for name := range r.Copies {
		copyPath = name
	}
	if interruption == "remote-ack-lost-then-deleted" {
		if err := os.Remove(filepath.Join(f.remoteRoot, copyPath)); err != nil {
			t.Fatal(err)
		}
	}
	f.probe.failReadAt, f.probe.lostACK = 0, false
	f.task.remote = f.probe
	f.restart(t)
	return f, request, copyPath
}

func TestPartialKeepBothRequiresExplicitCopyRecovery(t *testing.T) {
	for _, interruption := range []string{"after-local-copy", "remote-ack-lost-then-deleted", "parent-canceled", "parent-deadline"} {
		t.Run(interruption, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f, original, copyPath := partialCopyFixture(t, interruption)
				beforeWrites := f.probe.writes
				for range 2 {
					if err := f.task.reconcile(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				r := f.saved(t)
				if f.probe.writes != beforeWrites || r.Operations[copyPath] == nil || r.Conflicts[copyPath] == nil || r.Reason != "CONFLICT_COPY_REVIEW_REQUIRED" {
					t.Errorf("partial copy was not retained for explicit review: writes=%d reason=%s", f.probe.writes, r.Reason)
				}
				for _, name := range []string{copyPath, "file.txt"} {
					c := r.Conflicts[name]
					_, err := f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "repeat-both-" + c.Id})
					if connect.CodeOf(err) != connect.CodeFailedPrecondition || err.Error() != "failed_precondition: CONFLICT_COPY_REQUIRES_KEEP_LOCAL_OR_REMOTE" {
						t.Fatalf("unresolved copy allowed another keep-both decision: path=%s error=%v", name, err)
					}
				}
				if _, err := f.task.worker.ResolveConflict(t.Context(), original); connect.CodeOf(err) != connect.CodeAborted || f.probe.writes != beforeWrites || len(f.saved(t).Copies) != 1 {
					t.Fatal("unknown original request replayed copy side effects", err)
				}
				c := r.Conflicts[copyPath]
				recovery := &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: "recover-copy"}
				if _, err := f.task.worker.ResolveConflict(t.Context(), recovery); err != nil {
					t.Fatal("explicit copy direction could not recover", err)
				}
				writes := f.probe.writes
				f.restart(t)
				if _, err := f.task.worker.ResolveConflict(t.Context(), recovery); err != nil || f.probe.writes != writes {
					t.Fatal("completed copy resolution replayed a mutation", err)
				}
				for range 2 {
					if err := f.task.reconcile(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				r = f.saved(t)
				if r.Operations[copyPath] != nil || r.Conflicts[copyPath] != nil || r.Baselines[copyPath] == nil || r.Conflicts["file.txt"] == nil || len(r.Copies) != 1 {
					t.Fatal("copy recovery did not settle only the explicitly chosen path")
				}
				if contents(t, f.task.data.Mapping.LocalRoot, "file.txt") != "local conflict" || contents(t, f.remoteRoot, "file.txt") != "remote conflict" {
					t.Fatal("copy recovery silently chose the original conflict")
				}
				c = r.Conflicts["file.txt"]
				if _, err := f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: "recover-original"}); err != nil {
					t.Fatal(err)
				}
				if err := f.task.reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
				r = f.saved(t)
				if len(r.Operations) != 0 || len(r.Conflicts) != 0 || r.State != "active" || len(r.Copies) != 1 {
					t.Fatal("explicit keep-both recovery created a conflict loop")
				}
				for _, root := range []string{f.task.data.Mapping.LocalRoot, f.remoteRoot} {
					if contents(t, root, "file.txt") != "local conflict" || contents(t, root, copyPath) != "remote conflict" {
						t.Fatal("explicit recovery did not preserve both versions")
					}
				}
				entries, err := os.ReadDir(f.task.staging)
				if err != nil || len(entries) != 0 {
					t.Fatal("settled copy recovery retained private stages", err)
				}
			})
		})
	}
}

func TestPartialCopyReviewRejectsChangedGenerationAndNestedCopy(t *testing.T) {
	for _, side := range []string{"local", "remote"} {
		t.Run(side, func(t *testing.T) {
			f, _, copyPath := partialCopyFixture(t, "after-local-copy")
			if err := f.task.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			c := proto.Clone(f.saved(t).Conflicts[copyPath]).(*ppb.Conflict)
			root := f.task.data.Mapping.LocalRoot
			if side == "remote" {
				root = f.remoteRoot
			}
			write(t, root, copyPath, "new external copy edit")
			writes := f.probe.writes
			_, err := f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: "stale-copy"})
			if connect.CodeOf(err) != connect.CodeAborted || f.probe.writes != writes || contents(t, root, copyPath) != "new external copy edit" {
				t.Fatal("stale explicit recovery overwrote a modified copy", err)
			}
			c = f.saved(t).Conflicts[copyPath]
			_, err = f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "nested-copy"})
			if connect.CodeOf(err) != connect.CodeFailedPrecondition || f.probe.writes != writes || len(f.saved(t).Copies) != 1 {
				t.Fatal("unresolved copy generated a nested conflict copy", err)
			}
		})
	}
}

func TestCopyIntentBeforePrepareIsReobservedWithoutChoosingDirection(t *testing.T) {
	for _, side := range []string{"absent", "local", "remote", "ignored-local"} {
		t.Run(side, func(t *testing.T) {
			f := newJournalFixture(t)
			write(t, f.task.data.Mapping.LocalRoot, "file.txt", "local conflict")
			write(t, f.remoteRoot, "file.txt", "remote conflict")
			if err := f.task.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			c := f.saved(t).Conflicts["file.txt"]
			f.probe.failReadAt = 1
			_, err := f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "before-prepare"})
			if connect.CodeOf(err) != connect.CodeUnavailable {
				t.Fatal("source read did not interrupt before prepare", err)
			}
			r := f.saved(t)
			if len(r.Operations) != 0 || len(r.Copies) != 1 {
				t.Fatal("unexpected pre-prepare evidence")
			}
			var copyPath string
			for name := range r.Copies {
				copyPath = name
			}
			if side != "absent" {
				root := f.task.data.Mapping.LocalRoot
				if side == "remote" {
					root = f.remoteRoot
				}
				write(t, root, copyPath, "external copy")
				if side == "ignored-local" {
					write(t, root, ".shikiignore", copyPath+"\n")
				}
			}
			f.probe.failReadAt = 0
			f.restart(t)
			if err := f.task.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			r = f.saved(t)
			if f.probe.writes != 0 || f.probe.deletes != 0 {
				t.Fatal("copy lineage invented a recovery write direction")
			}
			if side == "absent" {
				if r.Baselines[copyPath] == nil || r.Baselines[copyPath].Exists || r.Conflicts[copyPath] != nil {
					t.Fatal("fresh matching absence did not settle unused copy intent")
				}
				c = r.Conflicts["file.txt"]
				if _, err := f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "fresh-choice"}); err != nil {
					t.Fatal("settled absent copy permanently blocked its original", err)
				}
			} else if r.Conflicts[copyPath] == nil || r.Reason != "CONFLICT_COPY_REVIEW_REQUIRED" || r.Baselines[copyPath] != nil {
				t.Fatal("unproven unilateral copy was silently accepted or mislabeled", r.Reason)
			}
		})
	}
}

func TestSettledCopyNewConflictStillAllowsKeepBoth(t *testing.T) {
	f := newJournalFixture(t)
	write(t, f.task.data.Mapping.LocalRoot, "file.txt", "local conflict")
	write(t, f.remoteRoot, "file.txt", "remote conflict")
	if err := f.task.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	c := f.saved(t).Conflicts["file.txt"]
	if _, err := f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "first-choice"}); err != nil {
		t.Fatal(err)
	}
	var copyPath string
	for name := range f.saved(t).Copies {
		copyPath = name
	}
	write(t, f.task.data.Mapping.LocalRoot, copyPath, "new local edit")
	write(t, f.remoteRoot, copyPath, "new remote edit")
	f.restart(t)
	if err := f.task.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := f.saved(t)
	c = r.Conflicts[copyPath]
	if c == nil || r.Reason != "CONFLICTS_PENDING" {
		t.Fatal("settled copy was still treated as an interrupted resolution", r.Reason)
	}
	if _, err := f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "new-copy-conflict"}); err != nil {
		t.Fatal("historical lineage prohibited a legitimate new conflict choice", err)
	}
	if err := f.task.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	r = f.saved(t)
	if len(r.Conflicts) != 0 || len(r.Operations) != 0 || len(r.Copies) != 2 {
		t.Fatal("new settled-copy resolution did not converge")
	}
	for _, root := range []string{f.task.data.Mapping.LocalRoot, f.remoteRoot} {
		if contents(t, root, copyPath) != "new local edit" {
			t.Fatal("new explicit local version not retained")
		}
		for name, original := range r.Copies {
			if original == copyPath && contents(t, root, name) != "new remote edit" {
				t.Fatal("new explicit remote version not preserved")
			}
		}
	}
}

func TestPartialCopyKeepRemoteExplicitlyAcceptsObservedDeletion(t *testing.T) {
	f, _, copyPath := partialCopyFixture(t, "remote-ack-lost-then-deleted")
	if err := f.task.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	c := f.saved(t).Conflicts[copyPath]
	if _, err := f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_REMOTE, RequestId: "accept-copy-deletion"}); err != nil {
		t.Fatal(err)
	}
	f.restart(t)
	if err := f.task.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := f.saved(t)
	if r.Operations[copyPath] != nil || r.Conflicts[copyPath] != nil || r.Baselines[copyPath] == nil || r.Baselines[copyPath].Exists || r.Conflicts["file.txt"] == nil {
		t.Fatal("explicit deletion did not settle only the reviewed copy")
	}
	for _, root := range []string{f.task.data.Mapping.LocalRoot, f.remoteRoot} {
		if _, err := os.Stat(filepath.Join(root, copyPath)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("explicit deletion resurrected the copy", err)
		}
	}
	if contents(t, f.task.data.Mapping.LocalRoot, "file.txt") != "local conflict" || contents(t, f.remoteRoot, "file.txt") != "remote conflict" {
		t.Fatal("copy deletion altered original versions")
	}
}
