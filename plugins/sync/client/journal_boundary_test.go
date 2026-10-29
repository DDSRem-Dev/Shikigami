package client

import (
	"bytes"
	"connectrpc.com/connect/v2"
	"fmt"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"os"
	"path/filepath"
	"testing"
)

func TestCommitPossibilityPersistedBeforeFinalPayloadOrEmptyHeader(t *testing.T) {
	for _, size := range []int{0, 768 << 10} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			f := newJournalFixture(t)
			data := bytes.Repeat([]byte("x"), size)
			if err := os.WriteFile(filepath.Join(f.task.data.Mapping.LocalRoot, "file.txt"), data, 0644); err != nil {
				t.Fatal(err)
			}
			var seen int64
			f.probe.onFrame = func(frame *pb.WriteFileRequest) {
				final := false
				if header := frame.GetHeader(); header != nil {
					final = header.Size == 0
				} else {
					seen += int64(len(frame.GetChunk()))
					final = seen == int64(size)
				}
				op := f.saved(t).Operations["file.txt"]
				if final && op.CommitState != "possible" || !final && op.CommitState != "incomplete" {
					t.Fatal("commit possibility was not durable before releasing complete input")
				}
			}
			if err := f.transfer(t, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProvenIncompleteInputCanRetryFreshUnlessIgnored(t *testing.T) {
	for _, mode := range []string{"existing-target", "absent-target", "new-ignore"} {
		t.Run(mode, func(t *testing.T) {
			f := newJournalFixture(t)
			task := f.task
			payload := bytes.Repeat([]byte("x"), 768<<10)
			if err := os.WriteFile(filepath.Join(task.data.Mapping.LocalRoot, "file.txt"), payload, 0644); err != nil {
				t.Fatal(err)
			}
			if mode == "absent-target" {
				if err := os.Remove(filepath.Join(f.remoteRoot, "file.txt")); err != nil {
					t.Fatal(err)
				}
				task.data.Baselines["file.txt"] = &ppb.FileVersion{}
				if err := task.persist(); err != nil {
					t.Fatal(err)
				}
			}
			f.probe.stopAfterChunks = 1
			if err := f.transfer(t, false); connect.CodeOf(err) != connect.CodeDataLoss {
				t.Fatal("short input did not fail before commit", err)
			}
			op := f.saved(t).Operations["file.txt"]
			if op.CommitState != "incomplete" || op.Phase != "prepared" {
				t.Fatal("partial body lacked no-commit proof")
			}
			if mode == "new-ignore" {
				write(t, task.data.Mapping.LocalRoot, ".shikiignore", "file.txt\n")
			}
			f.probe.stopAfterChunks = 0
			f.restart(t)
			for range 2 {
				if err := task.reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			r := f.saved(t)
			if mode == "new-ignore" {
				if f.probe.writes != 1 || r.Operations["file.txt"] == nil || contents(t, f.remoteRoot, "file.txt") != "baseline" {
					t.Fatal("recovery ignored changed ignore rules")
				}
			} else if f.probe.writes != 2 || r.Operations["file.txt"] != nil || contents(t, f.remoteRoot, "file.txt") != string(payload) {
				t.Fatal("safe incomplete-input retry failed")
			}
		})
	}
}
