package hostrotation_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

type partialFile struct {
	path string
	size int64
}

func findPartial(root, prefix string, payload []byte) (partialFile, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return partialFile{}, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return partialFile{}, err
		}
		if len(data) > 0 && len(data) < len(payload) && bytes.Equal(data, payload[:len(data)]) {
			return partialFile{path: path, size: int64(len(data))}, nil
		}
	}
	return partialFile{}, errors.New("no matching nonempty partial transfer")
}

func noTransferFiles(root, prefix string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			return fmt.Errorf("private transfer remains: %s", entry.Name())
		}
	}
	return nil
}

func (h *harness) baseline(mapping *ppb.Mapping, content []byte) *ppb.FileVersion {
	h.t.Helper()
	var baseline *ppb.FileVersion
	h.await("persisted common baseline", func(context.Context) error {
		data, err := os.ReadFile(filepath.Join(h.agentConfig.Agent.StateDir, "plugin-state", "sync", "ledger", "state.json"))
		if err != nil {
			return err
		}
		var snapshot storage.Snapshot
		if err = json.Unmarshal(data, &snapshot); err != nil {
			return err
		}
		var record struct {
			Mapping   *ppb.Mapping                `json:"mapping"`
			Baselines map[string]*ppb.FileVersion `json:"baselines"`
		}
		if err = snapshot.Get("sync_mappings", mapping.Id, &record); err != nil {
			return err
		}
		if !proto.Equal(record.Mapping, mapping) {
			return errors.New("worker mapping differs from desired mapping")
		}
		base := record.Baselines["flow.txt"]
		hash := sha256.Sum256(content)
		if base == nil || !base.Exists || base.ContentHash != hex.EncodeToString(hash[:]) || base.Revision == "" || base.Mode != 0600 {
			return errors.New("file common baseline not committed")
		}
		baseline = proto.Clone(base).(*ppb.FileVersion)
		return nil
	})
	return baseline
}

type publicFileObserver struct {
	allowNew atomic.Bool
	checks   atomic.Int64
	done     chan struct{}
	err      error // Published by closing done.
	cancel   context.CancelFunc
}

// Open/read checks observe an actual public inode, not just a size or timestamp.
// Before recovery is released it must remain the complete old file; afterwards
// every sampled version must be either the complete old or complete new file.
func observePublicFile(t *testing.T, parent context.Context, path string, old, next []byte) *publicFileObserver {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	o := &publicFileObserver{done: make(chan struct{}), cancel: cancel}
	oldHash, newHash := sha256.Sum256(old), sha256.Sum256(next)
	go func() {
		defer close(o.done)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			data, err := os.ReadFile(path)
			if err != nil {
				o.err = err
				return
			}
			hash := sha256.Sum256(data)
			if hash != oldHash && !(o.allowNew.Load() && hash == newHash) {
				o.err = fmt.Errorf("public destination published unexpected content: size=%d sha256=%x", len(data), hash)
				return
			}
			o.checks.Add(1)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-o.done })
	return o
}

func (o *publicFileObserver) check(t *testing.T) {
	t.Helper()
	select {
	case <-o.done:
		if o.err != nil {
			t.Fatal("atomic publication observation", o.err)
		}
	default:
	}
}
