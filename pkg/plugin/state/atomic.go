package state

import (
	"crypto/rand"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// AtomicWrite writes a basename into an already verified private directory.
// Pinning keeps temporary creation, rename, cleanup and fsync on the same inode.
func AtomicWrite(dir, name string, data []byte) (err error) {
	if len(data) > maxStateBytes {
		return ErrStateTooLarge
	}
	if !validStateName(name) {
		return errors.New("invalid state filename")
	}
	directory, err := openStateDirectory(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, directory.file.Close()) }()
	return directory.atomicWrite(name, data)
}
func (d *stateDirectory) atomicWrite(name string, data []byte) error {
	if len(data) > maxStateBytes {
		return ErrStateTooLarge
	}
	if !validStateName(name) {
		return errors.New("invalid state filename")
	}
	if err := d.check(); err != nil {
		return err
	}
	f, temp, err := d.createTemporary()
	if err != nil {
		return err
	}
	defer func() { _ = unix.Unlinkat(int(d.file.Fd()), temp, 0) }()
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = d.rename(temp, name); err != nil {
		return err
	}
	// Keep the existing file-then-directory durability order and propagate all
	// errors after rename as an uncertain commit to Store.Update's fail-close.
	return errors.Join(d.file.Sync(), d.check())
}
func (d *stateDirectory) createTemporary() (*os.File, string, error) {
	for range 100 {
		name := ".state-" + rand.Text()
		f, err := d.openPrivate(name, os.O_CREATE|os.O_EXCL|os.O_RDWR)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			cleanupErr := unix.Unlinkat(int(d.file.Fd()), name, 0)
			if !errors.Is(cleanupErr, os.ErrNotExist) {
				err = errors.Join(err, cleanupErr)
			}
		}
		return f, name, err
	}
	return nil, "", errors.New("cannot allocate unique state temporary file")
}
