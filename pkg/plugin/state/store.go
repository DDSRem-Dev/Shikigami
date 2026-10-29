// Package state implements a single-writer, versioned, crash-safe state store.
// Values contain persistent configuration only, never live process resources.
package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"time"
)

const SchemaVersion = 1
const maxStateBytes = 64 << 20

var (
	ErrLocked          = errors.New("state directory already has a writer")
	ErrCorrupt         = errors.New("state and backup are not valid; preserve files for recovery")
	ErrFutureSchema    = errors.New("state schema is newer than this program")
	ErrMigrationBackup = errors.New("cannot preserve pre-migration state")
	ErrStateTooLarge   = errors.New("state exceeds 64 MiB limit")
	ErrRequestMismatch = errors.New("request_id reused with different parameters")
	ErrClosed          = errors.New("state store is closed")
)

// Snapshot commits related resources and their request results together.
// Bucket names are owned by the service; records must be valid JSON objects.
type Snapshot struct {
	SchemaVersion int                                   `json:"schema_version"`
	Revision      uint64                                `json:"revision"`
	Buckets       map[string]map[string]json.RawMessage `json:"buckets"`
	Requests      map[string]RequestRecord              `json:"requests"`
}

type RequestRecord struct {
	Fingerprint string          `json:"fingerprint"`
	Status      string          `json:"status"` // pending, succeeded, failed
	ResourceID  string          `json:"resource_id,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	ErrorCode   string          `json:"error_code,omitempty"`
}

type Options struct {
	// SchemaVersion selects a service-owned schema; zero keeps the SDK default.
	// A writer never opens a schema newer than its configured target.
	SchemaVersion int
	// DisableRecovery is required for credential registries: restoring an old
	// backup could undo a token revocation. Such stores fail closed instead.
	DisableRecovery bool
	// Validate enforces application-specific references before any commit/recovery.
	Validate func(*Snapshot) error
	// Migrations are keyed by their source schema and must increment it by one.
	Migrations  map[int]func(*Snapshot) error
	LockTimeout time.Duration
}

type Store struct {
	directory     *stateDirectory
	mu            sync.Mutex
	dir           string
	lock          *os.File
	state         *Snapshot
	validate      func(*Snapshot) error
	schemaVersion int
	closed        bool
	recovered     bool
}

func Open(dir string, opts Options) (*Store, error) {
	if opts.SchemaVersion < 0 {
		return nil, errors.New("state schema version cannot be negative")
	}
	if err := EnsurePrivateDir(dir); err != nil {
		return nil, err
	}
	directory, err := openStateDirectory(dir)
	if err != nil {
		return nil, err
	}
	stat, ok := directory.info.Sys().(*syscall.Stat_t)
	if directory.info.Mode().Perm() != 0700 || !ok || stat.Uid != uint32(os.Geteuid()) {
		directory.file.Close()
		return nil, errors.New("state directory must be owned by this user and mode 0700")
	}
	lock, err := directory.openPrivate("state.lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		directory.file.Close()
		return nil, err
	}
	deadline := time.Now().Add(opts.LockTimeout)
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			lock.Close()
			directory.file.Close()
			return nil, err
		}
		if !time.Now().Before(deadline) {
			lock.Close()
			directory.file.Close()
			return nil, ErrLocked
		}
		time.Sleep(10 * time.Millisecond)
	}
	s := &Store{directory: directory, dir: dir, lock: lock, validate: opts.Validate, schemaVersion: opts.targetSchema()}
	state, err := directory.readSnapshot("state.json", opts)
	if errors.Is(err, os.ErrNotExist) {
		// Missing primary with an existing backup is recovery, not a fresh store.
		exists, backupErr := directory.exists("state.previous.json")
		if backupErr != nil {
			s.Close()
			return nil, backupErr
		}
		if !exists {
			state = &Snapshot{SchemaVersion: opts.targetSchema(), Buckets: map[string]map[string]json.RawMessage{}, Requests: map[string]RequestRecord{}}
			err = nil
		}
	}
	if errors.Is(err, ErrFutureSchema) || errors.Is(err, ErrMigrationBackup) || errors.Is(err, ErrStateTooLarge) {
		s.Close()
		return nil, err
	}
	if err != nil {
		if opts.DisableRecovery {
			s.Close()
			return nil, errors.Join(ErrCorrupt, err)
		}
		backup, backupErr := directory.readSnapshot("state.previous.json", opts)
		if backupErr != nil {
			s.Close()
			return nil, errors.Join(ErrCorrupt, err, backupErr)
		}
		// Preserve the damaged bytes before restoring the verified backup.
		exists, statErr := directory.exists("state.json")
		if statErr != nil {
			s.Close()
			return nil, statErr
		}
		if exists {
			if renameErr := directory.rename("state.json", fmt.Sprintf("state.corrupt.%d.json", time.Now().UnixNano())); renameErr != nil {
				s.Close()
				return nil, renameErr
			}
		}
		state, s.recovered = backup, true
	}
	s.state = state
	exists, statErr := directory.exists("state.json")
	if statErr != nil {
		s.Close()
		return nil, statErr
	}
	if !exists {
		data, marshalErr := marshalSnapshot(state, maxStateBytes)
		if marshalErr != nil {
			s.Close()
			return nil, marshalErr
		}
		if err := directory.atomicWrite("state.json", data); err != nil {
			s.Close()
			return nil, err
		}
	}
	if err := directory.check(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) RecoveredFromBackup() bool { return s.recovered }

// View returns an independent copy. Callers cannot mutate live state.
func (s *Store) View() (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	return clone(s.state)
}

// Update serializes reservation, validation and persistence as one transaction.
// An error returned after rename may mean the durable result is unknown; retry
// using the same request ID rather than repeating an external side effect.
func (s *Store) Update(fn func(*Snapshot) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if err := s.directory.check(); err != nil {
		s.closed = true
		return errors.Join(err, s.unlock())
	}
	next, err := clone(s.state)
	if err != nil {
		return err
	}
	if err = fn(next); err != nil {
		return err
	}
	if err := s.directory.check(); err != nil {
		s.closed = true
		return errors.Join(err, s.unlock())
	}
	next.SchemaVersion = s.schemaVersion
	next.Revision = s.state.Revision + 1
	if err = validateSnapshot(next, s.schemaVersion); err != nil {
		return err
	}
	if s.validate != nil {
		if err = s.validate(next); err != nil {
			return err
		}
	}
	previous, err := marshalSnapshot(s.state, maxStateBytes)
	if err != nil {
		return err
	}
	data, err := marshalSnapshot(next, maxStateBytes)
	if err != nil {
		return err
	}
	if err = s.directory.atomicWrite("state.previous.json", previous); err != nil {
		if errors.Is(err, ErrDirectoryChanged) {
			s.closed = true
			return errors.Join(err, s.unlock())
		}
		return err
	}
	if err = s.directory.atomicWrite("state.json", data); err != nil {
		// A rename followed by directory fsync failure is an uncertain commit.
		// Stop writes so no later transaction overwrites an unseen commit.
		s.closed = true
		unlockErr := s.unlock()
		return errors.Join(err, unlockErr)
	}
	s.state = next
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.unlock()
}

func (s *Store) unlock() error {
	var err error
	if s.lock != nil {
		err = errors.Join(syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN), s.lock.Close())
		s.lock = nil
	}
	if s.directory != nil {
		err = errors.Join(err, s.directory.file.Close())
		s.directory = nil
	}
	return err
}

func (s *Snapshot) Put(bucket, id string, value any) error {
	if bucket == "" || id == "" {
		return errors.New("bucket and id are required")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if s.Buckets == nil {
		s.Buckets = map[string]map[string]json.RawMessage{}
	}
	if s.Buckets[bucket] == nil {
		s.Buckets[bucket] = map[string]json.RawMessage{}
	}
	s.Buckets[bucket][id] = data
	return nil
}

// Get replaces value with the complete stored record. Decoding into a fresh
// destination prevents omitted fields and old map keys from surviving a read.
// Missing records and decoding errors leave the caller's value unchanged.
func (s *Snapshot) Get(bucket, id string, value any) error {
	data, ok := s.Buckets[bucket][id]
	if !ok {
		return os.ErrNotExist
	}
	destination := reflect.ValueOf(value)
	if !destination.IsValid() || destination.Kind() != reflect.Pointer || destination.IsNil() {
		return &json.InvalidUnmarshalError{Type: reflect.TypeOf(value)}
	}
	decoded := reflect.New(destination.Elem().Type())
	if err := json.Unmarshal(data, decoded.Interface()); err != nil {
		return err
	}
	destination.Elem().Set(decoded.Elem())
	return nil
}

// Request reserves a namespace-scoped idempotency key inside the transaction.
// A matching replay returns the original record and replay=true, even when the
// original operation is pending or failed. The caller persists completion later.
func (s *Snapshot) Request(namespace, id string, parameters any) (record RequestRecord, replay bool, err error) {
	if namespace == "" || id == "" || len(id) > 256 {
		return record, false, errors.New("valid namespace and request_id are required")
	}
	data, err := json.Marshal(parameters)
	if err != nil {
		return record, false, err
	}
	hash := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(hash[:])
	key := namespace + "\x00" + id
	if old, ok := s.Requests[key]; ok {
		if old.Fingerprint != fingerprint {
			return record, false, ErrRequestMismatch
		}
		return old, true, nil
	}
	record = RequestRecord{Fingerprint: fingerprint, Status: "pending"}
	if s.Requests == nil {
		s.Requests = map[string]RequestRecord{}
	}
	s.Requests[key] = record
	return record, false, nil
}

func (s *Snapshot) CompleteRequest(namespace, id, resourceID string, result any, errorCode string) error {
	key := namespace + "\x00" + id
	record, ok := s.Requests[key]
	if !ok {
		return errors.New("request was not reserved")
	}
	if record.Status != "pending" {
		return errors.New("request is already complete")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	record.Status = "succeeded"
	if errorCode != "" {
		record.Status = "failed"
	}
	record.ResourceID, record.Result, record.ErrorCode = resourceID, data, errorCode
	s.Requests[key] = record
	return nil
}

func clone(s *Snapshot) (*Snapshot, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var result Snapshot
	err = json.Unmarshal(data, &result)
	return &result, err
}

func (opts Options) targetSchema() int {
	if opts.SchemaVersion == 0 {
		return SchemaVersion
	}
	return opts.SchemaVersion
}

func readSnapshot(path string, opts Options) (*Snapshot, error) {
	d, err := openStateDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer d.file.Close()
	return d.readSnapshot(filepath.Base(path), opts)
}
func (d *stateDirectory) readSnapshot(name string, opts Options) (*Snapshot, error) {
	data, err := d.readPrivate(name)
	if err != nil {
		return nil, err
	}
	// A newer envelope may add fields unknown to this binary. Classify its
	// version before strict decoding so recovery cannot replace it with an old
	// backup merely because the new contract contains additional fields.
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	if header.SchemaVersion > opts.targetSchema() {
		return nil, ErrFutureSchema
	}
	var s Snapshot
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&s); err != nil {
		return nil, err
	}
	if err = dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing state data")
	}
	if s.SchemaVersion > opts.targetSchema() {
		return nil, ErrFutureSchema
	}
	originalVersion := s.SchemaVersion
	for s.SchemaVersion < opts.targetSchema() {
		version := s.SchemaVersion
		migrate := opts.Migrations[version]
		if migrate == nil {
			return nil, fmt.Errorf("no migration from schema %d", version)
		}
		if err = migrate(&s); err != nil {
			return nil, err
		}
		if s.SchemaVersion != version+1 {
			return nil, errors.New("migration did not increment schema")
		}
	}
	if err = validateSnapshot(&s, opts.targetSchema()); err != nil {
		return nil, err
	}
	if opts.Validate != nil {
		if err = opts.Validate(&s); err != nil {
			return nil, err
		}
	}
	if originalVersion < opts.targetSchema() {
		if _, err = marshalSnapshot(&s, maxStateBytes); err != nil {
			return nil, err
		}
		// Keep the exact old bytes independently of the rolling .previous
		// snapshot. Update will otherwise replace that snapshot with migrated
		// in-memory state, making rollback to the old schema impossible.
		if err = d.preserveMigrationSource(name, originalVersion, data); err != nil {
			return nil, errors.Join(ErrMigrationBackup, err)
		}
	}
	return &s, nil
}

func marshalSnapshot(state *Snapshot, limit int) ([]byte, error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, ErrStateTooLarge
	}
	return data, nil
}

func (d *stateDirectory) preserveMigrationSource(source string, version int, data []byte) error {
	sum := sha256.Sum256(data)
	name := fmt.Sprintf("%s.pre-migration-v%d-%x.json", filepath.Base(source), version, sum)
	exists, err := d.exists(name)
	if err == nil && exists {
		previous, err := d.readPrivate(name)
		if err != nil {
			return err
		}
		if !bytes.Equal(previous, data) {
			return errors.New("existing pre-migration backup differs; preserve it for diagnosis")
		}
	} else if err != nil {
		return err
	}
	// AtomicWrite creates mode 0600 and fsyncs both file and directory. Even an
	// existing identical backup is synced again, so a prior fsync error cannot
	// be mistaken for evidence that the rollback copy was made durable.
	return d.atomicWrite(name, data)
}

func validateSnapshot(s *Snapshot, target int) error {
	if s.SchemaVersion != target || s.Buckets == nil || s.Requests == nil {
		return errors.New("invalid state schema")
	}
	for _, bucket := range s.Buckets {
		for id, data := range bucket {
			if id == "" || !json.Valid(data) {
				return errors.New("invalid state record")
			}
		}
	}
	for _, record := range s.Requests {
		if len(record.Fingerprint) != 64 || (record.Status != "pending" && record.Status != "succeeded" && record.Status != "failed") {
			return errors.New("invalid request record")
		}
	}
	return nil
}

// EnsurePrivateDir rejects symlinks, foreign ownership and broad permissions.
func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return fmt.Errorf("state directory must be a real directory with mode 0700: %s", dir)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("state directory is not owned by this user")
	}
	return nil
}

func openPrivate(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || stat.Uid != uint32(os.Geteuid()) {
		f.Close()
		return nil, errors.New("state file must be owned by this user and mode 0600")
	}
	return f, nil
}

func ReadPrivateFile(path string) ([]byte, error) {
	f, err := openPrivate(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	closeErr := f.Close()
	if len(data) > maxStateBytes {
		return nil, ErrStateTooLarge
	}
	return data, errors.Join(readErr, closeErr)
}
