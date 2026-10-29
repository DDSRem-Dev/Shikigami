package ui

import (
	"os"
	"sync"

	"github.com/muesli/cancelreader"
)

// dashboardInput keeps native cancellation resources under our ownership.
// Bubble Tea 1.3.10 closes its native cancelreader without joining Read when
// killed (including context cancellation). This adapter intentionally has no
// Name method: it satisfies term.File for raw mode, but not cancelreader.File,
// so Bubble Tea only creates its resource-free fallback wrapper around it.
type dashboardInput struct {
	file   *os.File
	fd     uintptr
	reader cancelreader.CancelReader
	readMu sync.Mutex
	closed bool
	once   sync.Once
	err    error
}

func newDashboardInput(file *os.File) (*dashboardInput, error) {
	reader, err := cancelreader.NewReader(file)
	if err != nil {
		return nil, err
	}
	return &dashboardInput{file: file, fd: file.Fd(), reader: reader}, nil
}

func (r *dashboardInput) Fd() uintptr                    { return r.fd }
func (r *dashboardInput) Write(data []byte) (int, error) { return r.file.Write(data) }

func (r *dashboardInput) Read(data []byte) (int, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()
	if r.closed {
		return 0, cancelreader.ErrCanceled
	}
	return r.reader.Read(data)
}

func (r *dashboardInput) Close() error {
	r.once.Do(func() {
		// Wake the blocked native reader before joining it. Only after it has
		// returned may its epoll/kqueue and cancellation pipe be destroyed.
		r.reader.Cancel()
		r.readMu.Lock()
		defer r.readMu.Unlock()
		r.closed = true
		r.err = r.reader.Close()
	})
	return r.err
}
