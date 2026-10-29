package releaseauth

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

type privateOutput struct {
	path, name string
	parent     *os.Root
	root       *os.Root
	info       os.FileInfo
	files      []ownedFile
}

type ownedFile struct {
	name string
	info os.FileInfo
}

func (o *privateOutput) create(name string) (*os.File, error) {
	file, err := o.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fileError("new output file", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fileError("new output file identity", err)
	}
	o.files = append(o.files, ownedFile{name: name, info: info})
	return file, nil
}

func newOutput(path string) (_ *privateOutput, returnErr error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("invalid output path")
	}
	parent, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, fileError("output parent", err)
	}
	name := filepath.Base(absolute)
	if err = parent.Mkdir(name, 0700); err != nil {
		parent.Close()
		return nil, fileError("new output directory", err)
	}
	output := &privateOutput{path: absolute, name: name, parent: parent}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, output.remove())
			output.close()
		}
	}()
	output.info, err = parent.Lstat(name)
	if err != nil {
		return nil, fileError("new output identity", err)
	}
	if !output.info.IsDir() || output.info.Mode().Perm() != 0700 {
		return nil, errors.New("new output directory must be private mode 0700")
	}
	output.root, err = parent.OpenRoot(name)
	if err != nil {
		return nil, fileError("open new output directory", err)
	}
	if err = output.check(); err != nil {
		return nil, err
	}
	return output, nil
}

func (o *privateOutput) check() error {
	info, err := o.root.Stat(".")
	if err != nil || !os.SameFile(info, o.info) || info.Mode().Perm() != 0700 {
		return errors.New("private output directory identity or mode changed")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Uid) != uint64(os.Geteuid()) {
		return errors.New("private output directory owner changed")
	}
	current, err := os.Lstat(o.path)
	if err != nil || !os.SameFile(current, o.info) || !current.IsDir() {
		return errors.New("private output path changed")
	}
	for _, file := range o.files {
		current, err := o.root.Lstat(file.name)
		if err != nil || !os.SameFile(current, file.info) || !current.Mode().IsRegular() || current.Mode().Perm() != 0600 {
			return errors.New("private output file identity or mode changed")
		}
	}
	return nil
}

func (o *privateOutput) close() {
	if o.root != nil {
		o.root.Close()
	}
	o.parent.Close()
}

func (o *privateOutput) remove() error {
	var failures []error
	for _, file := range o.files {
		failures = append(failures, removeOwned(o.root, file.name, file.info))
	}
	if err := errors.Join(failures...); err != nil {
		return err
	}
	// Never recurse by a mutable pathname. Only the three owned files above
	// are removed through the pinned directory; the parent removes an empty
	// directory only while its observed identity still matches.
	return removeOwned(o.parent, o.name, o.info)
}

func removeOwned(parent *os.Root, name string, owned os.FileInfo) error {
	current, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || owned == nil || !os.SameFile(current, owned) {
		return errors.New("output changed; refusing to remove an unowned path")
	}
	err = parent.Remove(name)
	if err != nil {
		return fileError("remove failed output", err)
	}
	return syncRoot(parent)
}
