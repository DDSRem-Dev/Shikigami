package releaseauth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func replaceWith(t *testing.T, path, kind string) {
	t.Helper()
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	var err error
	switch kind {
	case "symlink":
		err = os.Symlink(path+".original", path)
	case "fifo":
		err = syscall.Mkfifo(path, 0600)
	case "directory":
		err = os.Mkdir(path, 0700)
	case "empty":
		err = os.WriteFile(path, nil, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestSpecialInputsAreRejectedWithoutBlocking(t *testing.T) {
	for _, operation := range []string{"sign-key", "sign-pin", "sign-archive", "verify-public", "verify-envelope", "verify-archive"} {
		for _, kind := range []string{"symlink", "fifo", "directory", "empty"} {
			t.Run(operation+"-"+kind, func(t *testing.T) {
				f := newFixture(t)
				if strings.HasPrefix(operation, "verify") {
					f.signed(t)
				}
				path := map[string]string{"sign-key": f.sign.PrivateKey, "sign-pin": f.sign.ChecksumsPin, "sign-archive": f.sign.Archive,
					"verify-public": f.verify.PublicKey, "verify-envelope": f.verify.Signature, "verify-archive": f.verify.Archive}[operation]
				replaceWith(t, path, kind)
				var err error
				if strings.HasPrefix(operation, "sign") {
					err = Sign(context.Background(), f.sign)
					absent(t, f.sign.Signature)
				} else {
					_, err = Verify(context.Background(), f.verify)
				}
				if err == nil {
					t.Fatal("unsafe input accepted")
				}
				absent(t, f.verify.Output)
			})
		}
	}
}

func TestPrivateKeyPermissionsAndOwner(t *testing.T) {
	for _, permission := range []os.FileMode{0644, 0400, 0000, 0600 | os.ModeSetuid, 0600 | os.ModeSetgid, 0600 | os.ModeSticky} {
		t.Run(permission.String(), func(t *testing.T) {
			f := newFixture(t)
			if err := os.Chmod(f.sign.PrivateKey, permission); err != nil {
				t.Fatal(err)
			}
			if err := Sign(context.Background(), f.sign); err == nil {
				t.Fatal("nonprivate key permissions accepted")
			}
			absent(t, f.sign.Signature)
		})
	}
	f := newFixture(t)
	if os.Geteuid() == 0 {
		if err := os.Chown(f.sign.PrivateKey, 1001, -1); err != nil {
			t.Fatal(err)
		}
	} else {
		// A real root-owned regular file demonstrates the ownership guard
		// without requiring an unprivileged test to change file ownership.
		f.sign.PrivateKey = "/etc/hosts"
	}
	if err := Sign(context.Background(), f.sign); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatal("foreign-owner private input was not rejected by ownership", err)
	}
}

func TestBoundsAndDuplicateInputs(t *testing.T) {
	for _, kind := range []string{"key", "public", "signature", "pin", "archive"} {
		t.Run("oversized-"+kind, func(t *testing.T) {
			f := newFixture(t)
			f.signed(t)
			path, size := f.sign.PrivateKey, int64(MaxMetadataBytes+1)
			switch kind {
			case "public":
				path = f.verify.PublicKey
			case "signature":
				path = f.sign.Signature
			case "pin":
				path, size = f.sign.ChecksumsPin, 66
			case "archive":
				path, size = f.sign.Archive, MaxArchiveBytes+1
			}
			if err := os.Truncate(path, size); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "key" || kind == "pin" {
				f.sign.Signature += ".new"
				err = Sign(context.Background(), f.sign)
				absent(t, f.sign.Signature)
			} else {
				_, err = Verify(context.Background(), f.verify)
			}
			if err == nil {
				t.Fatal("oversized input accepted")
			}
			absent(t, f.verify.Output)
		})
	}
	t.Run("hardlinked-pin-and-archive", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Remove(f.sign.Archive); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(f.sign.ChecksumsPin, f.sign.Archive); err != nil {
			t.Fatal(err)
		}
		if err := Sign(context.Background(), f.sign); err == nil || !strings.Contains(err.Error(), "distinct") {
			t.Fatal("hardlinked duplicate inputs were accepted", err)
		}
		absent(t, f.sign.Signature)
	})
}

func TestExistingOutputNeverOverwrittenOrRemoved(t *testing.T) {
	for _, operation := range []string{"sign", "verify"} {
		for _, kind := range []string{"file", "directory", "symlink", "fifo"} {
			t.Run(operation+"-"+kind, func(t *testing.T) {
				f := newFixture(t)
				path := f.sign.Signature
				if operation == "verify" {
					f.signed(t)
					path = f.verify.Output
				}
				sentinel := []byte("preexisting data must survive")
				writeFile(t, path, sentinel, 0600)
				if kind != "file" {
					replaceWith(t, path, kind)
				}
				before, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				if operation == "verify" {
					_, err = Verify(context.Background(), f.verify)
				} else {
					err = Sign(context.Background(), f.sign)
				}
				after, statErr := os.Lstat(path)
				if err == nil || statErr != nil || !os.SameFile(before, after) {
					t.Fatal("preexisting output changed or accepted", err, statErr)
				}
				if kind == "file" && !bytes.Equal(readFile(t, path), sentinel) {
					t.Fatal("preexisting output bytes changed")
				}
			})
		}
	}
}

func TestMetadataAuthenticatedBeforeArchiveAccess(t *testing.T) {
	f := newFixture(t)
	f.signed(t)
	other := newFixture(t)
	f.verify.PublicKey = other.verify.PublicKey
	replaceWith(t, f.verify.Archive, "fifo")
	if _, err := Verify(context.Background(), f.verify); err == nil || !strings.Contains(err.Error(), "signature authentication failed") {
		t.Fatal("archive accessed before rejecting untrusted metadata", err)
	}
	absent(t, f.verify.Output)
}

func TestVerifiedCopyUsesAlreadyOpenedArchiveStream(t *testing.T) {
	f := newFixture(t)
	f.signed(t)
	replaced := false
	ctx := actionContext{Context: context.Background(), action: func() {
		if replaced {
			return
		}
		// Creating the output is strictly after authentication and archive
		// open. Replace the input pathname before its first copied byte.
		if _, err := os.Stat(f.verify.Output); err == nil {
			replaced = true
			if err := os.Rename(f.verify.Archive, f.verify.Archive+".original"); err != nil {
				t.Fatal(err)
			}
			writeFile(t, f.verify.Archive, bytes.Repeat([]byte("x"), len(f.data)), 0600)
		}
	}}
	verified, err := Verify(ctx, f.verify)
	if err != nil || !replaced {
		t.Fatal("pinned input verification failed", err)
	}
	if !bytes.Equal(readFile(t, verified.Archive), f.data) || bytes.Equal(readFile(t, f.verify.Archive), f.data) {
		t.Fatal("verification reopened a changed input pathname")
	}
}

func TestReplacementPathsAreNotCreditedOrRecursivelyDeleted(t *testing.T) {
	t.Run("signature", func(t *testing.T) {
		f := newFixture(t)
		replaced := false
		ctx := actionContext{Context: context.Background(), action: func() {
			if replaced {
				return
			}
			if _, err := os.Stat(f.sign.Signature); err == nil {
				replaced = true
				if err := os.Rename(f.sign.Signature, f.sign.Signature+".moved"); err != nil {
					t.Fatal(err)
				}
				writeFile(t, f.sign.Signature, []byte("replacement must survive"), 0600)
			}
		}}
		if err := Sign(ctx, f.sign); err == nil || !replaced {
			t.Fatal("substituted signature path credited as success", err)
		}
		if string(readFile(t, f.sign.Signature)) != "replacement must survive" {
			t.Fatal("replacement signature was removed")
		}
	})
	t.Run("directory", func(t *testing.T) {
		f := newFixture(t)
		f.signed(t)
		replaced := false
		ctx := actionContext{Context: context.Background(), action: func() {
			if replaced {
				return
			}
			if info, err := os.Stat(filepath.Join(f.verify.Output, filepath.Base(f.sign.Archive))); err == nil && info.Size() > 0 {
				replaced = true
				if err := os.Rename(f.verify.Output, f.verify.Output+".moved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(f.verify.Output, 0700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(f.verify.Output, "unrelated"), []byte("preserve replacement directory"), 0600)
			}
		}}
		if result, err := Verify(ctx, f.verify); err == nil || !replaced || result != (Verified{}) {
			t.Fatal("substituted output directory credited as success", err)
		}
		if string(readFile(t, filepath.Join(f.verify.Output, "unrelated"))) != "preserve replacement directory" {
			t.Fatal("replacement directory was recursively deleted")
		}
		entries, err := os.ReadDir(f.verify.Output + ".moved")
		if err != nil || len(entries) != 0 {
			t.Fatal("owned files were not cleaned via pinned directory", err)
		}
	})
	t.Run("archive-output", func(t *testing.T) {
		f := newFixture(t)
		f.signed(t)
		path := filepath.Join(f.verify.Output, filepath.Base(f.sign.Archive))
		replaced := false
		ctx := actionContext{Context: context.Background(), action: func() {
			if replaced {
				return
			}
			if info, err := os.Stat(path); err == nil && info.Size() > 0 {
				replaced = true
				if err := os.Rename(path, path+".moved"); err != nil {
					t.Fatal(err)
				}
				writeFile(t, path, []byte("unowned replacement archive"), 0600)
			}
		}}
		if result, err := Verify(ctx, f.verify); err == nil || !replaced || result != (Verified{}) {
			t.Fatal("substituted archive copy credited as success", err)
		}
		if string(readFile(t, path)) != "unowned replacement archive" {
			t.Fatal("unowned replacement archive deleted")
		}
		absent(t, filepath.Join(f.verify.Output, "receipt.json"))
	})
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestStreamingBoundsAndShortWrites(t *testing.T) {
	var output bytes.Buffer
	if _, err := transfer(context.Background(), &output, strings.NewReader("12345"), 4); err == nil || output.Len() != 0 {
		t.Fatal("stream exceeded bound")
	}
	if _, err := transfer(context.Background(), shortWriter{}, strings.NewReader("12345"), 5); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short output write accepted", err)
	}
}
