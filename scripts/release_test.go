//go:build ignore

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func releaseFixture(t *testing.T) (string, string, string) {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	t.Chdir(filepath.Dir(filepath.Dir(source)))
	root := t.TempDir()
	bin, dist := filepath.Join(root, "bin"), filepath.Join(root, "dist")
	for _, dir := range []string{bin, dist} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"shiki", "shiki-agent", "shiki-daemon", "shiki-install", "shiki-plugin-sync", "shiki-plugin-port", "sync.manifest.json", "port.manifest.json"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("opaque fixture "+name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return bin, dist, "shikigami-v1.2.3-linux-arm64.tar.gz"
}

func TestReleaseRejectsExistingOutputsWithoutChangingThem(t *testing.T) {
	for _, suffix := range []string{"", ".sha256", ".checksums-pin"} {
		for _, kind := range []string{"file", "symlink", "directory"} {
			t.Run(suffix+"/"+kind, func(t *testing.T) {
				bin, dist, name := releaseFixture(t)
				path := filepath.Join(dist, name+suffix)
				sentinel := []byte("existing user data must survive\n")
				check := path
				switch kind {
				case "file":
					if err := os.WriteFile(path, sentinel, 0600); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					check = filepath.Join(t.TempDir(), "target")
					if err := os.WriteFile(check, sentinel, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(check, path); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					check = filepath.Join(path, "sentinel")
					if err := os.WriteFile(check, sentinel, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := run(bin, dist, "v1.2.3", "linux", "arm64"); err == nil {
					t.Fatal("existing output accepted")
				}
				got, err := os.ReadFile(check)
				if err != nil || !bytes.Equal(got, sentinel) {
					t.Fatal("existing file or symlink target changed", err)
				}
				if kind == "symlink" {
					if target, err := os.Readlink(path); err != nil || target != check {
						t.Fatal("symlink replaced", err)
					}
				}
				entries, err := os.ReadDir(dist)
				if err != nil || len(entries) != 1 || entries[0].Name() != name+suffix {
					t.Fatalf("partial newly-created outputs survived: %v %v", entries, err)
				}
			})
		}
	}
}

func TestReleaseSidecarsDescribeCompletedArchive(t *testing.T) {
	bin, dist, name := releaseFixture(t)
	if err := run(bin, dist, "v1.2.3", "linux", "arm64"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dist, name))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	sidecar, err := os.ReadFile(filepath.Join(dist, name+".sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sidecar) != hex.EncodeToString(digest[:])+"  "+name+"\n" {
		t.Fatal("archive digest does not match")
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	var sums []byte
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "SHA256SUMS" {
			sums, err = io.ReadAll(archive)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(sums) == 0 {
		t.Fatal("missing checksum manifest")
	}
	pin, err := os.ReadFile(filepath.Join(dist, name+".checksums-pin"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(sums)
	if string(pin) != hex.EncodeToString(sum[:])+"\n" {
		t.Fatal("pin differs from archived checksum manifest")
	}
	entries, err := os.ReadDir(dist)
	if err != nil || len(entries) != 3 {
		t.Fatal("unexpected outputs", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatal("output is not private regular file", err)
		}
	}
	if err := run(bin, dist, "v1.2.3", "linux", "arm64"); err == nil {
		t.Fatal("repeat overwrote archive")
	}
	if got, err := os.ReadFile(filepath.Join(dist, name+".checksums-pin")); err != nil || strings.TrimSpace(string(got)) != hex.EncodeToString(sum[:]) {
		t.Fatal("repeat changed pin", err)
	}
}

func TestReleaseCleanupPreservesReplacedPaths(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.OpenFile("owned", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := os.Rename(filepath.Join(dir, "owned"), filepath.Join(dir, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "owned"), 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, "owned", "unrelated")
	if err := os.WriteFile(sentinel, []byte("preserve replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupReleaseOutputs(root, []releaseOutput{{name: "owned", info: info}}); err == nil {
		t.Fatal("replacement cleanup did not report changed identity")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "preserve replacement" {
		t.Fatal("cleanup changed replacement", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "moved")); err != nil {
		t.Fatal("cleanup followed moved file", err)
	}
}
