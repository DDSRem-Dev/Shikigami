package filetree

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/reconcile"
	"github.com/fsnotify/fsnotify"
)

const watchDebounce = 100 * time.Millisecond
const watchRetryDelay = 30 * time.Second
const maxWatchDirectories = 1024

type watchSource interface {
	Add(string) error
	Remove(string) error
	Close() error
	events() <-chan fsnotify.Event
	errors() <-chan error
}
type nativeWatch struct{ *fsnotify.Watcher }

func (w *nativeWatch) events() <-chan fsnotify.Event { return w.Events }
func (w *nativeWatch) errors() <-chan error          { return w.Errors }
func newNativeWatch() (watchSource, error) {
	w, err := fsnotify.NewBufferedWatcher(256)
	if err != nil {
		return nil, err
	}
	return &nativeWatch{w}, nil
}

type recursiveWatch struct {
	tree    *Tree
	request *pb.WatchFilesRequest
	start   string
	source  watchSource
	matcher *reconcile.Ignore
	watched map[string]bool
}

// WatchFiles emits bounded rescan hints. One worker subscription owns one
// recursive native watcher; frontends never create a local filesystem watcher.
// The periodic hint remains authoritative when events are lost or unsupported.
func (t *Tree) WatchFiles(ctx context.Context, r *pb.WatchFilesRequest, send func(*pb.FileEvent) error) error {
	return t.watchFiles(ctx, r, send, newNativeWatch)
}
func (t *Tree) watchFiles(ctx context.Context, r *pb.WatchFilesRequest, send func(*pb.FileEvent) error, factory func() (watchSource, error)) error {
	if r == nil {
		return failure(connect.CodeInvalidArgument, "request required")
	}
	if err := t.check(ctx, r.ProjectId); err != nil {
		return err
	}
	start, err := clean(r.Path, true)
	if err != nil {
		return err
	}
	directory, err := t.directory(start, false)
	if err != nil {
		return err
	}
	directory.Close()
	session := &recursiveWatch{tree: t, request: r, start: start, watched: map[string]bool{}}
	var events <-chan fsnotify.Event
	var failures <-chan error
	var retryAt time.Time
	retire := func() {
		if session.source != nil {
			session.source.Close()
		}
		session.source = nil
		session.watched = map[string]bool{}
		events = nil
		failures = nil
		retryAt = time.Now().Add(watchRetryDelay)
	}
	defer func() {
		if session.source != nil {
			session.source.Close()
		}
	}()
	refresh := func() {
		if session.source == nil {
			if time.Now().Before(retryAt) {
				return
			}
			source, err := factory()
			if err != nil {
				retire()
				return
			}
			session.source = source
			events = source.events()
			failures = source.errors()
		}
		if err := session.refresh(ctx); err != nil {
			retire()
		}
	}
	refresh()
	emit := func() error {
		if err := t.check(ctx, r.ProjectId); err != nil {
			return err
		}
		return send(&pb.FileEvent{Type: pb.FileEvent_RESCAN_REQUIRED, Path: start, ProjectId: t.opts.ProjectID, Timestamp: time.Now().UnixNano()})
	}
	if err = emit(); err != nil {
		return err
	}
	ticker := time.NewTicker(t.opts.PollInterval)
	defer ticker.Stop()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var debounce <-chan time.Time
	rebuild := false
	schedule := func(register bool) {
		rebuild = rebuild || register
		if debounce == nil {
			timer.Reset(watchDebounce)
			debounce = timer.C
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.closed:
			return failure(connect.CodeUnavailable, "filesystem closed")
		case event, ok := <-events:
			if !ok {
				retire()
				schedule(false)
				continue
			}
			if session.relevant(event) {
				schedule(event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 || filepath.Base(event.Name) == ".shikiignore")
			}
		case _, ok := <-failures:
			if !ok {
				retire()
				schedule(false)
				continue
			}
			// Overflow and every watcher error invalidate event completeness. Rebuild
			// registrations and rescan; no event is ever interpreted as a file deletion.
			schedule(true)
		case <-debounce:
			debounce = nil
			if rebuild {
				refresh()
				rebuild = false
			}
			if err = emit(); err != nil {
				return err
			}
		case <-ticker.C:
			if debounce != nil {
				timer.Stop()
				debounce = nil
			}
			rebuild = false
			refresh()
			if err = emit(); err != nil {
				return err
			}
		}
	}
}
func (w *recursiveWatch) relevant(event fsnotify.Event) bool {
	relative, err := filepath.Rel(w.tree.root.Name(), event.Name)
	if err != nil {
		return false
	}
	relative = filepath.ToSlash(relative)
	if relative != "." {
		if _, err = clean(relative, false); err != nil {
			return false
		}
	}
	if w.start != "." && relative != w.start && !strings.HasPrefix(relative, w.start+"/") {
		return false
	}
	if relative == "." || relative == ".shikiignore" {
		return true
	}
	directory := w.watched[event.Name]
	if info, err := os.Lstat(event.Name); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		directory = info.IsDir()
	}
	return w.matcher == nil || !w.matcher.Ignored(relative, directory)
}
func (w *recursiveWatch) refresh(ctx context.Context) error {
	matcher, err := w.tree.Ignore(ctx, w.request.Ignore)
	if err != nil {
		return err
	}
	w.matcher = matcher
	desired := map[string]os.FileInfo{}
	queue := []string{w.start}
	entries := 0
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative := queue[0]
		queue = queue[1:]
		if relative != "." && matcher.Ignored(relative, true) {
			continue
		}
		if len(desired) >= maxWatchDirectories {
			return failure(connect.CodeResourceExhausted, "directory watch limit exceeded")
		}
		directory, err := w.tree.directory(relative, false)
		if err != nil {
			return err
		}
		info, err := directory.Stat()
		if err != nil {
			directory.Close()
			return err
		}
		children, readErr := directory.ReadDir(w.tree.opts.MaxEntries + 1)
		directory.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		entries += len(children)
		if entries > w.tree.opts.MaxEntries {
			return failure(connect.CodeResourceExhausted, "watch scan entry limit exceeded")
		}
		absolute := filepath.Join(w.tree.root.Name(), filepath.FromSlash(relative))
		desired[absolute] = info
		for _, child := range children {
			if child.Type()&os.ModeSymlink != 0 || !child.IsDir() {
				continue
			}
			name := path.Join(relative, child.Name())
			if matcher.Ignored(name, true) {
				continue
			}
			queue = append(queue, name)
		}
	}
	// On Linux a renamed directory keeps its inotify descriptor. Remove stale
	// names before adding their replacements: Add(new) followed by Remove(old)
	// can otherwise remove the same inode's newly named registration.
	for absolute := range w.watched {
		if desired[absolute] == nil {
			_ = w.source.Remove(absolute)
		}
	}
	registered := map[string]bool{}
	for absolute, info := range desired {
		// Native paths only emit hints; data access stays on nofollow handles.
		if err = w.source.Add(absolute); err != nil {
			return err
		}
		current, err := os.Lstat(absolute)
		if err != nil || !current.IsDir() || !os.SameFile(info, current) {
			_ = w.source.Remove(absolute)
			return failure(connect.CodeAborted, "watch directory changed")
		}
		registered[absolute] = true
	}
	w.watched = registered
	return nil
}

// Ignore reads policy configuration through the pinned root independently of
// the automatic file-transfer cap. The policy file always has its own 1 MiB cap.
func (t *Tree) Ignore(ctx context.Context, extra []string) (*reconcile.Ignore, error) {
	if err := t.check(ctx, t.opts.ProjectID); err != nil {
		return nil, err
	}
	directory, err := t.directory(".", false)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	file, err := openRegular(directory, ".shikiignore")
	if connect.CodeOf(err) == connect.CodeNotFound {
		return reconcile.NewIgnore(strings.NewReader(strings.Join(extra, "\n")))
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if before.Size() > 1<<20 {
		return nil, failure(connect.CodeResourceExhausted, "ignore file too large")
	}
	matcher, err := reconcile.NewIgnore(io.MultiReader(io.LimitReader(file, (1<<20)+1), strings.NewReader("\n"+strings.Join(extra, "\n"))))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !sameStat(before, after) {
		return nil, failure(connect.CodeAborted, "ignore file changed")
	}
	return matcher, ctx.Err()
}
