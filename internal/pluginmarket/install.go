package pluginmarket

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
)

const installJournal = ".install-transaction.json"

// InstallTransaction is a durable publication and runtime-restoration intent.
// Only private basenames are stored, never arbitrary paths. Until Finish, the
// previous good package remains recoverable even after a committed replacement.
type InstallTransaction struct {
	Format                  int    `json:"format"`
	ID                      string `json:"id"`
	Operation               string `json:"operation"`
	Phase                   string `json:"phase"`
	Stage                   string `json:"stage"`
	Backup                  string `json:"backup"`
	RestoreEnabled          bool   `json:"restore_enabled"`
	RuntimeRevision         string `json:"runtime_revision"`
	RuntimeAction           string `json:"runtime_action"`
	RuntimeRequestID        string `json:"runtime_request_id"`
	RuntimeExpectedRevision string `json:"runtime_expected_revision"`
}

// InstallStore serializes installers, updates, uninstalls and their recovery.
// The host does not acquire this lock: callers may use host IPC while holding it.
type InstallStore struct {
	Base      string
	lock      *os.File
	directory *os.File
	info      os.FileInfo
}

func OpenInstallStore(ctx context.Context, base string) (*InstallStore, error) {
	if info, err := os.Lstat(base); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("plugins_dir %q is a legacy release symlink; migrate the managed installation to a private plugin directory with the current installer before modifying third-party plugins", base)
	}
	if err := state.EnsurePrivateDir(base); err != nil {
		return nil, err
	}
	directory, err := os.OpenFile(base, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := directory.Stat()
	if err != nil {
		directory.Close()
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(base, ".install.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		directory.Close()
		return nil, err
	}
	store := &InstallStore{Base: base, lock: lock, directory: directory, info: info}
	stat, err := lock.Stat()
	if err == nil {
		owner, ok := stat.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Geteuid()) || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 {
			err = errors.New("unsafe plugin installation lock")
		}
	}
	if err != nil {
		store.Close()
		return nil, err
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			store.Close()
			return nil, err
		}
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if err := store.check(); err != nil {
				store.Close()
				return nil, err
			}
			parent, err := os.Open(filepath.Dir(base))
			if err == nil {
				err = errors.Join(parent.Sync(), parent.Close(), store.Sync())
			}
			if err != nil {
				store.Close()
				return nil, err
			}
			return store, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			store.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			store.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *InstallStore) Close() error {
	return errors.Join(s.lock.Close(), s.directory.Close())
}

func (s *InstallStore) check() error {
	current, err := os.Lstat(s.Base)
	if err != nil || !current.IsDir() || current.Mode().Perm() != 0700 || !os.SameFile(s.info, current) {
		return errors.New("plugin installation directory changed")
	}
	owner, ok := current.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return errors.New("plugin installation directory owner changed")
	}
	return nil
}

func (s *InstallStore) Sync() error {
	if err := s.check(); err != nil {
		return err
	}
	return s.directory.Sync()
}

func (s *InstallStore) Pending() (*InstallTransaction, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(s.Base, installJournal), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errors.New("unsafe plugin installation journal")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("plugin installation journal has a foreign owner")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return nil, errors.New("invalid plugin installation journal")
	}
	var tx InstallTransaction
	if err := decodeExactObject(data, []string{"format", "id", "operation", "phase", "stage", "backup", "restore_enabled", "runtime_revision", "runtime_action", "runtime_request_id", "runtime_expected_revision"}, &tx); err != nil {
		return nil, fmt.Errorf("invalid plugin installation journal: %w", err)
	}
	if err := validateTransaction(tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

func validateTransaction(tx InstallTransaction) error {
	if tx.Format != 1 || ValidatePluginID(tx.ID) != nil || tx.ID == "sync" || tx.ID == "port" {
		return errors.New("invalid plugin installation transaction")
	}
	if tx.Operation != "install" && tx.Operation != "update" && tx.Operation != "uninstall" {
		return errors.New("invalid plugin installation operation")
	}
	if tx.Phase != "prepared" && tx.Phase != "committed" && tx.Phase != "rolled-back" {
		return errors.New("invalid plugin installation phase")
	}
	if !privateInstallName(tx.Backup, ".plugin-backup-") || (tx.Operation != "uninstall" && !privateInstallName(tx.Stage, ".plugin-stage-")) || (tx.Operation == "uninstall" && tx.Stage != "") {
		return errors.New("invalid plugin installation paths")
	}
	if len(tx.RuntimeRevision) > 4096 || len(tx.RuntimeExpectedRevision) > 4096 || len(tx.RuntimeRequestID) > 128 {
		return errors.New("invalid plugin runtime transaction revision")
	}
	if tx.RuntimeAction == "" {
		if tx.RuntimeRequestID != "" || tx.RuntimeExpectedRevision != "" {
			return errors.New("incomplete plugin runtime transaction")
		}
	} else if (tx.RuntimeAction != "disable" && tx.RuntimeAction != "enable") || tx.RuntimeRequestID == "" || tx.RuntimeExpectedRevision == "" {
		return errors.New("invalid plugin runtime transaction action")
	}
	return nil
}

// PrepareRuntimeAction saves an idempotent IPC request before it can be sent.
func (s *InstallStore) PrepareRuntimeAction(tx *InstallTransaction, action string) error {
	if tx.RuntimeAction != "" {
		return errors.New("a plugin runtime action is already pending")
	}
	tx.RuntimeAction = action
	tx.RuntimeRequestID = rand.Text()
	tx.RuntimeExpectedRevision = tx.RuntimeRevision
	return s.save(tx)
}

// ObserveRuntimeAction records acceptance even if process readiness is pending.
func (s *InstallStore) ObserveRuntimeAction(tx *InstallTransaction, revision string, complete bool) error {
	tx.RuntimeRevision = revision
	if complete {
		tx.RuntimeAction, tx.RuntimeRequestID, tx.RuntimeExpectedRevision = "", "", ""
	}
	return s.save(tx)
}

// AdoptRuntimeState preserves a newer explicit user mutation instead of
// restoring an obsolete enabled intent after an interrupted transaction.
func (s *InstallStore) AdoptRuntimeState(tx *InstallTransaction, revision string, enabled bool) error {
	tx.RuntimeRevision = revision
	tx.RuntimeAction, tx.RuntimeRequestID, tx.RuntimeExpectedRevision = "", "", ""
	if tx.Operation != "install" || tx.Phase == "committed" {
		tx.RestoreEnabled = enabled
	}
	return s.save(tx)
}

func privateInstallName(name, prefix string) bool {
	return strings.HasPrefix(name, prefix) && len(name) > len(prefix) && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`)
}

func (s *InstallStore) save(tx *InstallTransaction) error {
	if err := validateTransaction(*tx); err != nil {
		return err
	}
	if err := s.check(); err != nil {
		return err
	}
	data, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	return state.AtomicWrite(s.Base, installJournal, data)
}

func (s *InstallStore) Begin(id, operation, stage string, restoreEnabled bool, revision string) (*InstallTransaction, error) {
	pending, err := s.Pending()
	if err != nil {
		return nil, err
	}
	if pending != nil {
		return nil, errors.New("plugin installation recovery is required")
	}
	tx := &InstallTransaction{Format: 1, ID: id, Operation: operation, Phase: "prepared", Stage: stage, Backup: ".plugin-backup-" + rand.Text(), RestoreEnabled: restoreEnabled, RuntimeRevision: revision}
	if err := s.save(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

func (s *InstallStore) rename(old, new string) error {
	if err := s.check(); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(s.Base, old), filepath.Join(s.Base, new)); err != nil {
		return err
	}
	return s.Sync()
}

// Publish first durably saves the old package privately, then publishes a fully
// synced stage and persists its commit decision. Recovery rolls back any gap.
func (s *InstallStore) Publish(tx *InstallTransaction) error {
	if tx.Phase != "prepared" {
		return errors.New("plugin transaction is not prepared")
	}
	if err := validateTransaction(*tx); err != nil {
		return err
	}
	if tx.Operation != "install" {
		if err := s.rename(tx.ID, tx.Backup); err != nil {
			return fmt.Errorf("save previous plugin: %w", err)
		}
	}
	if tx.Operation != "uninstall" {
		if err := s.rename(tx.Stage, tx.ID); err != nil {
			return fmt.Errorf("publish plugin: %w", err)
		}
	}
	tx.Phase = "committed"
	return s.save(tx)
}

func (s *InstallStore) exists(name string) (bool, error) {
	info, err := os.Lstat(filepath.Join(s.Base, name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("unsafe plugin transaction directory")
	}
	return true, nil
}

// RecoverPublication reverts an interrupted uncommitted publication. Committed
// packages remain in place, with their backup retained until runtime completion.
// The caller must stop the plugin before recovery and restore its enabled intent
// over IPC before Finish; the host never infers desired state from this journal.
func (s *InstallStore) RecoverPublication(tx *InstallTransaction) error {
	if err := validateTransaction(*tx); err != nil {
		return err
	}
	if tx.Phase == "committed" {
		published, err := s.exists(tx.ID)
		if err != nil {
			return err
		}
		if tx.Operation == "uninstall" {
			if published {
				return errors.New("committed plugin uninstall has an unexpected public package; backup retained")
			}
		} else if !published {
			if tx.Operation == "update" {
				return s.Rollback(tx)
			}
			return errors.New("committed plugin installation is missing; transaction retained")
		}
		return nil
	}
	if tx.Phase == "rolled-back" {
		return nil
	}
	backup, err := s.exists(tx.Backup)
	if err != nil {
		return err
	}
	dest, err := s.exists(tx.ID)
	if err != nil {
		return err
	}
	if backup {
		if dest {
			stage, err := s.exists(tx.Stage)
			if err != nil || stage || tx.Operation == "uninstall" {
				return errors.New("ambiguous interrupted plugin publication; previous package retained")
			}
			if err := s.rename(tx.ID, tx.Stage); err != nil {
				return err
			}
		}
		if err := s.rename(tx.Backup, tx.ID); err != nil {
			return err
		}
	} else if tx.Operation == "install" && dest {
		stage, err := s.exists(tx.Stage)
		if err != nil || stage {
			return errors.New("ambiguous interrupted first plugin installation")
		}
		if err := s.rename(tx.ID, tx.Stage); err != nil {
			return err
		}
	} else if tx.Operation != "install" && !dest {
		return errors.New("previous plugin package is missing; transaction retained")
	}
	tx.Phase = "rolled-back"
	return s.save(tx)
}

// Rollback retains a committed update's previous package when activation fails.
func (s *InstallStore) Rollback(tx *InstallTransaction) error {
	if tx.Operation != "update" || tx.Phase != "committed" {
		return errors.New("only a committed update can be rolled back")
	}
	backup, err := s.exists(tx.Backup)
	if err != nil || !backup {
		return errors.New("previous plugin package is unavailable for rollback")
	}
	tx.Phase = "prepared"
	if err := s.save(tx); err != nil {
		return err
	}
	return s.RecoverPublication(tx)
}

func (s *InstallStore) Finish(tx *InstallTransaction) error {
	if tx.Phase != "committed" && tx.Phase != "rolled-back" {
		return errors.New("plugin transaction has not completed")
	}
	if err := validateTransaction(*tx); err != nil {
		return err
	}
	if err := s.check(); err != nil {
		return err
	}
	for _, name := range []string{tx.Stage, tx.Backup} {
		if name != "" {
			if err := os.RemoveAll(filepath.Join(s.Base, name)); err != nil {
				return err
			}
		}
	}
	if err := s.Sync(); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.Base, installJournal)); err != nil {
		return err
	}
	return s.Sync()
}
