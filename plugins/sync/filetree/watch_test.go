package filetree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/fsnotify/fsnotify"
)

func waitWatch(t *testing.T, events <-chan *pb.FileEvent) {
	t.Helper()
	select {
	case event := <-events:
		if event.Type != pb.FileEvent_RESCAN_REQUIRED {
			t.Fatal("filesystem hint was treated as an authoritative mutation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch hint timeout")
	}
}
func waitRegistered(t *testing.T, source *nativeWatch, absolute string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, name := range source.WatchList() {
			if name == absolute {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("new nested directory not registered")
}
func TestRecursiveNativeWatcherRegistersNewDirectoriesAndSkipsPrivateRoots(t *testing.T) {
	tree, root, _ := fixture(t, func(opts *Options) { opts.PollInterval = time.Hour })
	for _, name := range []string{"existing/deep", ".git/private", ".shiki-sync-private", "ignored/deep"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".shikiignore"), []byte("ignored/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "external")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan *pb.FileEvent, 20)
	created := make(chan *nativeWatch, 1)
	done := make(chan error, 1)
	go func() {
		done <- tree.watchFiles(ctx, &pb.WatchFilesRequest{ProjectId: "p"}, func(event *pb.FileEvent) error {
			select {
			case events <- event:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, func() (watchSource, error) {
			source, err := newNativeWatch()
			if err == nil {
				created <- source.(*nativeWatch)
			}
			return source, err
		})
	}()
	source := <-created
	waitWatch(t, events)
	canonical := tree.root.Name()
	waitRegistered(t, source, filepath.Join(canonical, "existing", "deep"))
	for _, name := range source.WatchList() {
		relative, _ := filepath.Rel(canonical, name)
		for _, blocked := range []string{".git", ".shiki-sync-private", "ignored", "external"} {
			if relative == blocked || strings.HasPrefix(relative, blocked+string(filepath.Separator)) {
				t.Fatalf("private/ignored/symlink directory watched: %s", relative)
			}
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "new", "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	waitRegistered(t, source, filepath.Join(canonical, "new", "deep"))
	waitWatch(t, events)
	if err := os.WriteFile(filepath.Join(root, "new", "deep", "file.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWatch(t, events)
	if err := os.Rename(filepath.Join(root, "new"), filepath.Join(root, "renamed")); err != nil {
		t.Fatal(err)
	}
	waitRegistered(t, source, filepath.Join(canonical, "renamed", "deep"))
	waitWatch(t, events)
	// Editing ignore rules changes registrations rather than only filtering hints.
	if err := os.WriteFile(filepath.Join(root, ".shikiignore"), []byte("ignored/\nrenamed/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitWatch(t, events)
	deadline := time.Now().Add(3 * time.Second)
	for {
		found := false
		for _, name := range source.WatchList() {
			if strings.HasPrefix(name, filepath.Join(canonical, "renamed")) {
				found = true
			}
		}
		if !found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("new ignore rule retained directory watches")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("native watcher did not cancel")
	}
	if len(source.WatchList()) != 0 {
		t.Fatal("native registrations survived cancellation")
	}
}

type fakeWatch struct {
	event   chan fsnotify.Event
	failure chan error
	mu      sync.Mutex
	paths   map[string]bool
	closed  atomic.Int32
}

func newFakeWatch() *fakeWatch {
	return &fakeWatch{event: make(chan fsnotify.Event, 512), failure: make(chan error, 4), paths: map[string]bool{}}
}
func (w *fakeWatch) Add(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paths[path] = true
	return nil
}
func (w *fakeWatch) Remove(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.paths, path)
	return nil
}
func (w *fakeWatch) Close() error                  { w.closed.Add(1); return nil }
func (w *fakeWatch) events() <-chan fsnotify.Event { return w.event }
func (w *fakeWatch) errors() <-chan error          { return w.failure }
func TestWatcherOverflowAndClosedChannelsRequireRescanWithoutSpin(t *testing.T) {
	tree, _, _ := fixture(t, func(opts *Options) { opts.PollInterval = time.Hour })
	source := newFakeWatch()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan *pb.FileEvent, 20)
	done := make(chan error, 1)
	var factories atomic.Int32
	go func() {
		done <- tree.watchFiles(ctx, &pb.WatchFilesRequest{ProjectId: "p"}, func(event *pb.FileEvent) error {
			select {
			case events <- event:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, func() (watchSource, error) { factories.Add(1); return source, nil })
	}()
	waitWatch(t, events)
	source.failure <- fsnotify.ErrEventOverflow
	waitWatch(t, events)
	for i := 0; i < 200; i++ {
		source.event <- fsnotify.Event{Name: filepath.Join(tree.root.Name(), "changed.txt"), Op: fsnotify.Write}
	}
	waitWatch(t, events)
	close(source.event)
	close(source.failure)
	waitWatch(t, events)
	time.Sleep(150 * time.Millisecond)
	if factories.Load() != 1 || source.closed.Load() != 1 {
		t.Fatal("closed backend was immediately restarted or repeatedly closed")
	}
	if len(events) > 2 {
		t.Fatal("closed watcher channels caused a busy loop")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("fallback watcher did not cancel")
	}
}
func TestWatcherUnavailableFallsBackAndSendFailureClosesNativeSource(t *testing.T) {
	tree, _, _ := fixture(t, func(opts *Options) { opts.PollInterval = 20 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	count := 0
	factories := 0
	err := tree.watchFiles(ctx, &pb.WatchFilesRequest{ProjectId: "p"}, func(event *pb.FileEvent) error {
		count++
		if count == 3 {
			cancel()
		}
		return nil
	}, func() (watchSource, error) { factories++; return nil, errors.New("native watcher unavailable") })
	if !errors.Is(err, context.Canceled) || count != 3 || factories != 1 {
		t.Fatal("native failure disabled polling or caused unbounded retries")
	}
	source := newFakeWatch()
	sentinel := errors.New("consumer stopped")
	err = tree.watchFiles(context.Background(), &pb.WatchFilesRequest{ProjectId: "p"}, func(*pb.FileEvent) error { return sentinel }, func() (watchSource, error) { return source, nil })
	if !errors.Is(err, sentinel) || source.closed.Load() != 1 {
		t.Fatal("send failure leaked native watcher")
	}
}
