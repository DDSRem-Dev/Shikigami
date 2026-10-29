//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const rotationRuntimeMaximumBytes int64 = 512 << 20
const rotationRuntimeMaximumFiles = 8192

// The outer Linux collector removes its exact container even on failure. Keep
// private runtime evidence in its mounted evidence directory first. Do not
// follow links or copy live sockets; retain their relative names as metadata.
func retainRotationRuntime(root, evidence string) (string, error) {
	destination, err := os.MkdirTemp(evidence, "private-rotation-runtime-")
	if err != nil {
		return "", err
	}
	var skipped []string
	hashes := map[string]string{}
	var total int64
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		output := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.Mkdir(output, 0700)
		}
		if !entry.Type().IsRegular() {
			skipped = append(skipped, relative)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if len(hashes) >= rotationRuntimeMaximumFiles || info.Size() > rotationRuntimeMaximumBytes-total {
			return fmt.Errorf("private retention limit exceeded at %s", relative)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		digest := sha256.New()
		if _, err := io.CopyN(io.MultiWriter(file, digest), input, info.Size()); err != nil {
			return err
		}
		var extra [1]byte
		if n, err := input.Read(extra[:]); n != 0 || err != io.EOF {
			return fmt.Errorf("runtime file changed during retention: %s", relative)
		}
		total += info.Size()
		if err := file.Close(); err != nil {
			return err
		}
		hashes[relative] = fmt.Sprintf("%x", digest.Sum(nil))
		return nil
	})
	receipt := map[string]any{"original_root": root, "retained_root": destination, "files_sha256": hashes, "skipped_nonregular": skipped, "complete": err == nil, "copied_bytes": total, "maximum_bytes": rotationRuntimeMaximumBytes, "maximum_files": rotationRuntimeMaximumFiles}
	if err != nil {
		receipt["error"] = err.Error()
	}
	data, encodeErr := json.MarshalIndent(receipt, "", "  ")
	if encodeErr != nil {
		return destination, encodeErr
	}
	if saveErr := os.WriteFile(filepath.Join(destination, "retention.json"), data, 0600); saveErr != nil {
		return destination, saveErr
	}
	return destination, err
}

func TestDefaultRotationFailureEvidenceRetainsPrivateFilesWithoutFollowingLinks(t *testing.T) {
	root, evidence := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "state"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state", "fixture"), []byte("private fixture evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "state", "fixture"), filepath.Join(root, "untrusted-link")); err != nil {
		t.Fatal(err)
	}
	retained, err := retainRotationRuntime(root, evidence)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(retained)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("retained directory is not private", err)
	}
	data, err := os.ReadFile(filepath.Join(retained, "state", "fixture"))
	if err != nil || string(data) != "private fixture evidence" {
		t.Fatal("retained content differs", err)
	}
	if _, err := os.Lstat(filepath.Join(retained, "untrusted-link")); !os.IsNotExist(err) {
		t.Fatal("retention followed or copied a symlink", err)
	}
	var receipt struct {
		Complete bool              `json:"complete"`
		Skipped  []string          `json:"skipped_nonregular"`
		Hashes   map[string]string `json:"files_sha256"`
	}
	data, err = os.ReadFile(filepath.Join(retained, "retention.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Complete || len(receipt.Skipped) != 1 || receipt.Skipped[0] != "untrusted-link" || receipt.Hashes["state/fixture"] != fmt.Sprintf("%x", sha256.Sum256([]byte("private fixture evidence"))) {
		t.Fatal("incomplete private retention receipt", receipt)
	}
}

func TestDefaultRotationFailureEvidenceRejectsOversizedRuntime(t *testing.T) {
	root, evidence := t.TempDir(), t.TempDir()
	file, err := os.Create(filepath.Join(root, "oversized"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(rotationRuntimeMaximumBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	retained, err := retainRotationRuntime(root, evidence)
	if err == nil {
		t.Fatal("oversized failed runtime copied without bound")
	}
	data, readErr := os.ReadFile(filepath.Join(retained, "retention.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	var receipt struct {
		Complete bool  `json:"complete"`
		Copied   int64 `json:"copied_bytes"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Complete || receipt.Copied != 0 {
		t.Fatal("incomplete retention was presented as complete", receipt)
	}
	if _, err := os.Stat(filepath.Join(root, "oversized")); err != nil {
		t.Fatal("retention removed original failure evidence", err)
	}
}
