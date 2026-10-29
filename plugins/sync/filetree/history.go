package filetree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"time"

	"connectrpc.com/connect/v2"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
)

const pendingBucket = "filetree_pending"
const historyBucket = "filetree_history"

type pending struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Temp string `json:"temp"`
}

// HistoryRecord identifies an ordinary, private content file under StateDir/history.
// Oldest versions are discarded when the per-path count or total byte budget is
// needed for a subsequent mutation; preserved files are not a permanent archive.
type HistoryRecord struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Revision string `json:"revision"`
	Created  int64  `json:"created"`
}

// A failed preservation must be repaired before another mutation can consume
// more project space or evict a retained version.
func (t *Tree) ensureRecovered() error {
	s, err := t.state.View()
	if err != nil {
		return err
	}
	if len(s.Buckets[pendingBucket]) != 0 {
		return failure(connect.CodeFailedPrecondition, "pending displaced file requires recovery before further mutations")
	}
	return nil
}

func (t *Tree) putPending(p pending) error {
	return t.state.Update(func(s *storage.Snapshot) error { return s.Put(pendingBucket, p.ID, p) })
}
func (t *Tree) clearPending(id string) error {
	return t.state.Update(func(s *storage.Snapshot) error { delete(s.Buckets[pendingBucket], id); return nil })
}
func (t *Tree) History() ([]HistoryRecord, error) {
	s, err := t.state.View()
	if err != nil {
		return nil, err
	}
	var records []HistoryRecord
	for _, b := range s.Buckets[historyBucket] {
		var r HistoryRecord
		if err = json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Created == records[j].Created {
			return records[i].ID < records[j].ID
		}
		return records[i].Created < records[j].Created
	})
	return records, nil
}
func (t *Tree) reserve(p string, bytes int64) error {
	records, err := t.History()
	if err != nil {
		return err
	}
	var total int64
	count := 0
	for _, r := range records {
		total += r.Size
		if r.Path == p {
			count++
		}
	}
	for _, r := range records {
		if total+bytes <= t.opts.HistoryBytes && count < t.opts.HistoryVersions {
			break
		}
		if total+bytes <= t.opts.HistoryBytes && r.Path != p {
			continue
		}
		if err = t.history.Remove(r.ID); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err = t.state.Update(func(s *storage.Snapshot) error { delete(s.Buckets[historyBucket], r.ID); return nil }); err != nil {
			return err
		}
		total -= r.Size
		if r.Path == p {
			count--
		}
	}
	if total+bytes > t.opts.HistoryBytes {
		return failure(connect.CodeResourceExhausted, "history capacity exhausted")
	}
	return nil
}

// A pending record is durable before rename. On a crash we preserve whichever
// regular inode remains at its unique temporary name, never overwrite the user's
// current path. A failed preservation leaves the temporary file and journal.
func (t *Tree) preserve(d *os.File, j pending) (string, error) {
	source, err := openRegular(d, j.Temp)
	if err != nil {
		return "", err
	}
	defer source.Close()
	v, err := t.inspect(context.Background(), source, j.Path)
	if err != nil {
		return "", err
	}
	if v.Size > t.opts.MaxFileBytes {
		return "", failure(connect.CodeResourceExhausted, "displaced file exceeds history limit; retained in project temporary file")
	}
	if err = t.reserve(j.Path, v.Size); err != nil {
		return "", err
	}
	id := j.ID
	out, err := t.history.OpenFile(id, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) { // Previous crash may have left an incomplete copy; source is still retained.
		if err = t.history.Remove(id); err != nil {
			return "", err
		}
		out, err = t.history.OpenFile(id, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	}
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		out.Close()
		if !complete {
			_ = t.history.Remove(id)
		}
	}()
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	n, err := io.CopyBuffer(out, io.LimitReader(source, t.opts.MaxFileBytes+1), make([]byte, ChunkSize))
	if err != nil {
		return "", err
	}
	if n != v.Size {
		return "", failure(connect.CodeAborted, "displaced file changed while preserving")
	}
	if err = out.Sync(); err != nil {
		return "", err
	}
	if err = out.Close(); err != nil {
		return "", err
	}
	copyFile, err := t.history.Open(id)
	if err != nil {
		return "", err
	}
	copyInfo, err := t.inspect(context.Background(), copyFile, j.Path)
	copyFile.Close()
	if err != nil {
		return "", err
	}
	after, err := t.inspect(context.Background(), source, j.Path)
	if err != nil {
		return "", err
	}
	if copyInfo.ContentHash != v.ContentHash || after.Revision != v.Revision {
		return "", failure(connect.CodeAborted, "displaced file changed while preserving")
	}
	hd, err := t.history.Open(".")
	if err != nil {
		return "", err
	}
	err = hd.Sync()
	hd.Close()
	if err != nil {
		return "", err
	}
	record := HistoryRecord{ID: id, Path: j.Path, Size: n, Revision: v.Revision, Created: time.Now().UnixNano()}
	if err = t.state.Update(func(s *storage.Snapshot) error { return s.Put(historyBucket, id, record) }); err != nil {
		return "", err
	}
	complete = true
	if err = removeAt(d, j.Temp); err != nil {
		return "", err
	}
	if err = d.Sync(); err != nil {
		return "", err
	}
	if err = t.clearPending(j.ID); err != nil {
		return "", err
	}
	return v.Revision, nil
}
func (t *Tree) recover() error {
	s, err := t.state.View()
	if err != nil {
		return err
	}
	for id, b := range s.Buckets[pendingBucket] {
		var j pending
		if err = json.Unmarshal(b, &j); err != nil {
			return err
		}
		p, e := clean(j.Path, false)
		if e != nil || id != j.ID || j.Temp != j.ID || len(j.Temp) != len(internalPrefix)+32 || j.Temp[:len(internalPrefix)] != internalPrefix {
			return fmt.Errorf("invalid pending filesystem transaction")
		}
		d, e := t.directory(path.Dir(p), false)
		if e != nil {
			return fmt.Errorf("recover displaced version: %w", e)
		}
		_, e = t.preserve(d, j)
		d.Close()
		if connect.CodeOf(e) == connect.CodeNotFound {
			e = t.clearPending(id)
		}
		if e != nil {
			return fmt.Errorf("recover displaced version: %w", e)
		}
	}
	return nil
}
