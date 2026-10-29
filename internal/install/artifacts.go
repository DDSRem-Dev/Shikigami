package install

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type Artifacts struct {
	root   *os.Root
	lock   *os.File
	Prefix string
}

func OpenArtifacts(prefix string) (*Artifacts, error) {
	if !filepath.IsAbs(prefix) || filepath.Clean(prefix) == string(filepath.Separator) {
		return nil, errors.New("installation prefix must be an absolute private directory")
	}
	if os.Geteuid() == 0 {
		if err := ensureSystemPrefix(prefix); err != nil {
			return nil, err
		}
	} else if err := os.MkdirAll(prefix, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(prefix)
	if err != nil {
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || !info.IsDir() || (info.Mode().Perm() != 0700 && !(os.Geteuid() == 0 && info.Mode().Perm() == 0755)) {
		return nil, errors.New("installation prefix must be owned by the installer, non-symlink and private (root system prefix may be 0755)")
	}
	root, err := os.OpenRoot(prefix)
	if err != nil {
		return nil, err
	}
	pinned, err := root.Stat(".")
	if err != nil || !os.SameFile(info, pinned) {
		root.Close()
		return nil, errors.New("installation prefix changed")
	}
	lock, err := root.OpenFile("install.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		root.Close()
		return nil, err
	}
	lockInfo, err := lock.Stat()
	if err != nil || !lockInfo.Mode().IsRegular() || lockInfo.Mode().Perm() != 0600 {
		lock.Close()
		root.Close()
		return nil, errors.New("unsafe installation lock")
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		root.Close()
		return nil, errors.New("another installation transaction is active")
	}
	a := &Artifacts{root: root, lock: lock, Prefix: prefix}
	if err = root.MkdirAll("releases", 0700); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}
func (a *Artifacts) Close() error   { return errors.Join(a.lock.Close(), a.root.Close()) }
func releaseName(m Manifest) string { return filepath.Join("releases", m.Version+"-"+m.OS+"-"+m.Arch) }
func (a *Artifacts) Stage(bundle *Bundle) (string, error) {
	destination := releaseName(bundle.Manifest)
	if _, err := a.root.Lstat(destination); err == nil {
		verified, err := Verify(filepath.Join(a.Prefix, destination), bundle.Manifest.Version, bundle.TrustedChecksums)
		if err != nil {
			return "", err
		}
		// A prior attempt may have published the directory but failed to sync
		// its parent. Verification alone does not establish crash durability.
		if err = a.syncExistingRelease(destination, verified.Manifest); err != nil {
			return "", err
		}
		return destination, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	needs, err := a.stageNeeds(bundle)
	if err != nil {
		return "", err
	}
	if err = checkSpace(context.Background(), needs); err != nil {
		return "", err
	}
	staging := ".stage-" + rand.Text()
	if err := a.root.Mkdir(staging, 0700); err != nil {
		return "", err
	}
	defer a.root.RemoveAll(staging)
	target, err := a.root.OpenRoot(staging)
	if err != nil {
		return "", err
	}
	defer target.Close()
	source, err := os.OpenRoot(bundle.Directory)
	if err != nil {
		return "", err
	}
	defer source.Close()
	for _, entry := range bundle.Manifest.Files {
		if err = copyPayload(source, target, entry); err != nil {
			return "", err
		}
	}
	for _, name := range []string{ManifestName, ChecksumsName} {
		data, err := readRegular(bundle.Directory, name, 64<<10)
		if err != nil {
			return "", err
		}
		if name == ChecksumsName && digest(data) != bundle.TrustedChecksums {
			return "", errors.New("bundle checksum changed while staging")
		}
		file, err := target.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
		if err != nil {
			return "", err
		}
		_, err = file.Write(data)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err = errors.Join(err, closeErr); err != nil {
			return "", err
		}
	}
	if _, err = Verify(filepath.Join(a.Prefix, staging), bundle.Manifest.Version, bundle.TrustedChecksums); err != nil {
		return "", err
	}
	if err = syncDir(target); err != nil {
		return "", err
	}
	if err = a.root.Rename(staging, destination); err != nil {
		return "", err
	}
	if err = a.syncReleaseParents(); err != nil {
		return "", err
	}
	return destination, nil
}

func (a *Artifacts) syncExistingRelease(destination string, manifest Manifest) error {
	target, err := a.root.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer target.Close()
	files := make([]string, 0, len(manifest.Files)+2)
	for _, entry := range manifest.Files {
		files = append(files, entry.Name)
	}
	files = append(files, ManifestName, ChecksumsName)
	for _, name := range files {
		file, err := target.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		err = file.Sync()
		if err = errors.Join(err, file.Close()); err != nil {
			return err
		}
	}
	if err = syncDir(target); err != nil {
		return err
	}
	return a.syncReleaseParents()
}

func (a *Artifacts) syncReleaseParents() error {
	// The cross-directory rename changes both releases/ and the prefix.
	releases, err := a.root.OpenRoot("releases")
	if err != nil {
		return err
	}
	defer releases.Close()
	if err = syncDir(releases); err != nil {
		return err
	}
	return syncDir(a.root)
}
func copyPayload(source, target *os.Root, entry File) error {
	info, err := source.Lstat(entry.Name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("release payload is not a regular file")
	}
	in, err := source.OpenFile(entry.Name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("release payload changed during staging")
	}
	mode := os.FileMode(0600)
	if entry.Executable {
		mode = 0700
	}
	out, err := target.OpenFile(entry.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(in, entry.Size+1))
	if err != nil {
		return err
	}
	if n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
		return errors.New("payload changed or failed checksum during staging")
	}
	if err = out.Sync(); err != nil {
		return err
	}
	return out.Close()
}
func syncDir(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func (a *Artifacts) Current() (string, error) {
	target, err := a.root.Readlink("current")
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !validReleasePath(target) {
		return "", errors.New("active installation link is not a managed release")
	}
	return target, nil
}
func validReleasePath(target string) bool {
	return filepath.Dir(target) == "releases" && filepath.Base(target) != "." && filepath.Clean(target) == target
}
func (a *Artifacts) switchTo(target string) error {
	if target == "" {
		err := a.root.Remove("current")
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !validReleasePath(target) {
		return errors.New("invalid release selection")
	}
	info, err := a.root.Lstat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("selected release is not a directory")
	}
	temporary := ".current-" + rand.Text()
	if err = a.root.Symlink(target, temporary); err != nil {
		return err
	}
	defer a.root.Remove(temporary)
	if err = a.root.Rename(temporary, "current"); err != nil {
		return err
	}
	return syncDir(a.root)
}
func (a *Artifacts) writeJSON(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := ".receipt-" + rand.Text()
	f, err := a.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer a.root.Remove(temporary)
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = a.root.Rename(temporary, name); err != nil {
		return err
	}
	return syncDir(a.root)
}
func (a *Artifacts) readJSON(name string, value any) error {
	info, err := a.root.Lstat(name)
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0600 || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return fmt.Errorf("invalid %s", name)
	}
	f, err := a.root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("installation metadata too large")
	}
	return decode(data, value)
}
