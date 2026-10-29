//go:build ignore

// Package the already built fixed-version local payloads. This produces local
// integrity metadata, not publisher signatures or an authenticated download URL.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
)

func main() {
	directory := flag.String("dir", "bin", "built payload directory")
	output := flag.String("output", "dist", "release output directory")
	version := flag.String("version", "", "fixed release version")
	goos := flag.String("os", runtime.GOOS, "target OS")
	arch := flag.String("arch", runtime.GOARCH, "target architecture")
	flag.Parse()
	if err := run(*directory, *output, *version, *goos, *arch); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(directory, output, version, goos, arch string) (returnErr error) {
	for _, name := range []string{"install.sh", "install-client.sh", "uninstall.sh"} {
		data, err := os.ReadFile(filepath.Join("scripts", name))
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(directory, name), data, 0700); err != nil {
			return err
		}
	}
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(directory, "LICENSE"), license, 0600); err != nil {
		return err
	}
	pin, err := install.CreateManifest(directory, version, goos, arch)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(output, 0700); err != nil {
		return err
	}
	name := fmt.Sprintf("shikigami-%s-%s-%s.tar.gz", version, goos, arch)
	target := filepath.Join(output, name)
	root, err := os.OpenRoot(output)
	if err != nil {
		return err
	}
	defer root.Close()
	var created []releaseOutput
	defer func() {
		for _, entry := range created {
			entry.file.Close()
		}
		if returnErr != nil {
			returnErr = errors.Join(returnErr, cleanupReleaseOutputs(root, created))
		}
	}()
	// Reserve every output before writing any archive bytes. O_EXCL rejects
	// existing files and symlinks; a pinned directory keeps cleanup local.
	for _, suffix := range []string{"", ".sha256", ".checksums-pin"} {
		file, err := root.OpenFile(name+suffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		created = append(created, releaseOutput{name: name + suffix, file: file, info: info})
		if err != nil {
			return err
		}
		if err := file.Chmod(0600); err != nil {
			return err
		}
	}
	file := created[0].file
	h := sha256.New()
	compressed := gzip.NewWriter(io.MultiWriter(file, h))
	compressed.Header.ModTime = time.Unix(0, 0)
	archive := tar.NewWriter(compressed)
	data, err := os.ReadFile(filepath.Join(directory, install.ManifestName))
	if err != nil {
		return err
	}
	var manifest install.Manifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	names := []string{install.ManifestName, install.ChecksumsName}
	for _, entry := range manifest.Files {
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		in, err := os.Open(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		info, err := in.Stat()
		if err != nil {
			in.Close()
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			in.Close()
			return err
		}
		header.Name = name
		header.ModTime = time.Unix(0, 0)
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		if err = archive.WriteHeader(header); err != nil {
			in.Close()
			return err
		}
		_, err = io.Copy(archive, in)
		in.Close()
		if err != nil {
			return err
		}
	}
	if err = archive.Close(); err != nil {
		return err
	}
	if err = compressed.Close(); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = finishReleaseSidecar(created[1].file, hex.EncodeToString(h.Sum(nil))+"  "+name+"\n"); err != nil {
		return err
	}
	if err = finishReleaseSidecar(created[2].file, pin+"\n"); err != nil {
		return err
	}
	for _, entry := range created {
		info, err := root.Lstat(entry.name)
		if err != nil || !os.SameFile(info, entry.info) || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return errors.New("release output identity or mode changed")
		}
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	fmt.Printf("Local artifact: %s\nLocal SHA256SUMS digest: %s\nThese locally computed hashes are not publisher signatures.\n", target, pin)
	return nil
}

type releaseOutput struct {
	name string
	file *os.File
	info os.FileInfo
}

func finishReleaseSidecar(file *os.File, text string) error {
	if _, err := io.WriteString(file, text); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return file.Close()
}

func cleanupReleaseOutputs(root *os.Root, created []releaseOutput) error {
	var failures []error
	for _, entry := range created {
		current, err := root.Lstat(entry.name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || entry.info == nil || !os.SameFile(current, entry.info) {
			failures = append(failures, errors.New("release output changed; refusing cleanup of replacement"))
			continue
		}
		// Nonrecursive cleanup of our observed files, not an atomic conditional
		// unlink against an adversary with write access to the output directory.
		if err := root.Remove(entry.name); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
