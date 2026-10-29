package releaseauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	root    string
	sign    SignOptions
	verify  VerifyOptions
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	data    []byte
	pin     string
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: root, private: private, public: public, data: bytes.Repeat([]byte("opaque archive bytes; never unpack or execute\n"), 1024)}
	f.sign = SignOptions{Version: "v1.2.3", OS: "linux", Arch: "arm64", Archive: filepath.Join(root, "shikigami-v1.2.3-linux-arm64.tar.gz"),
		ChecksumsPin: filepath.Join(root, "archive.checksums-pin"), PrivateKey: filepath.Join(root, "private.pem"), Signature: filepath.Join(root, "archive.signature.json")}
	f.verify = VerifyOptions{Version: f.sign.Version, OS: f.sign.OS, Arch: f.sign.Arch, Archive: f.sign.Archive,
		Signature: f.sign.Signature, PublicKey: filepath.Join(root, "public.pem"), Output: filepath.Join(root, "verified")}
	sum := sha256.Sum256([]byte("generated SHA256SUMS fixture"))
	f.pin = hex.EncodeToString(sum[:])
	writeFile(t, f.sign.Archive, f.data, 0600)
	writeFile(t, f.sign.ChecksumsPin, []byte(f.pin+"\n"), 0600)
	writeFile(t, f.sign.PrivateKey, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0600)
	writeFile(t, f.verify.PublicKey, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0644)
	return f
}

func (f *fixture) signed(t *testing.T) {
	t.Helper()
	if err := Sign(context.Background(), f.sign); err != nil {
		t.Fatal(err)
	}
}

func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected absent output, got %v", err)
	}
}

func mode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != want {
		t.Fatalf("incorrect output mode: %v %v", info, err)
	}
}

func TestSignVerifyCreatesAuthenticatedPrivateCopy(t *testing.T) {
	f := newFixture(t)
	f.signed(t)
	result, err := Verify(context.Background(), f.verify)
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != Format || result.Version != f.sign.Version || result.OS != f.sign.OS || result.Arch != f.sign.Arch || result.ChecksumsSHA256 != f.pin || result.ArchiveSize != int64(len(f.data)) || !filepath.IsAbs(result.Archive) {
		t.Fatal("verified result differs from signed release")
	}
	if !bytes.Equal(readFile(t, result.Archive), f.data) || !bytes.Equal(readFile(t, f.sign.Archive), f.data) {
		t.Fatal("copied bytes or original archive changed")
	}
	sum := sha256.Sum256(f.data)
	if result.ArchiveSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("receipt archive hash differs")
	}
	der, _ := x509.MarshalPKIXPublicKey(f.public)
	fingerprint := sha256.Sum256(der)
	if result.KeyFingerprintSHA256 != hex.EncodeToString(fingerprint[:]) {
		t.Fatal("receipt public key fingerprint differs")
	}
	if string(readFile(t, filepath.Join(f.verify.Output, "checksums-pin"))) != f.pin+"\n" {
		t.Fatal("trusted pin differs")
	}
	var receipt Verified
	if err = json.Unmarshal(readFile(t, filepath.Join(f.verify.Output, "receipt.json")), &receipt); err != nil || receipt != result {
		t.Fatal("receipt differs from completed verification", err)
	}
	mode(t, f.verify.Output, 0700)
	for _, name := range []string{result.Archive, filepath.Join(f.verify.Output, "checksums-pin"), filepath.Join(f.verify.Output, "receipt.json"), f.sign.Signature} {
		mode(t, name, 0600)
	}
	entries, err := os.ReadDir(f.verify.Output)
	if err != nil || len(entries) != 3 {
		t.Fatal("unexpected extraction or output files", err)
	}
	before := readFile(t, f.sign.Signature)
	if err = Sign(context.Background(), f.sign); err == nil || !bytes.Equal(before, readFile(t, f.sign.Signature)) {
		t.Fatal("repeated signing overwrote existing signature")
	}
	if _, err = Verify(context.Background(), f.verify); err == nil || !bytes.Equal(readFile(t, result.Archive), f.data) {
		t.Fatal("repeated verification overwrote existing output")
	}
}

func TestWrongKeyAndArchiveTamperingFailClosed(t *testing.T) {
	for _, test := range []string{"wrong-key", "same-size-byte-change", "truncated", "appended", "wrong-version", "wrong-os", "wrong-arch", "wrong-basename"} {
		t.Run(test, func(t *testing.T) {
			f := newFixture(t)
			f.signed(t)
			switch test {
			case "wrong-key":
				other := newFixture(t)
				f.verify.PublicKey = other.verify.PublicKey
			case "same-size-byte-change":
				data := append([]byte(nil), f.data...)
				data[len(data)/2] ^= 1
				writeFile(t, f.sign.Archive, data, 0600)
			case "truncated":
				writeFile(t, f.sign.Archive, f.data[:len(f.data)-1], 0600)
			case "appended":
				writeFile(t, f.sign.Archive, append(f.data, 1), 0600)
			case "wrong-version":
				f.verify.Version = "v9.9.9"
				f.verify.Archive = filepath.Join(f.root, "shikigami-v9.9.9-linux-arm64.tar.gz")
			case "wrong-os":
				f.verify.OS = "darwin"
				f.verify.Archive = filepath.Join(f.root, "shikigami-v1.2.3-darwin-arm64.tar.gz")
			case "wrong-arch":
				f.verify.Arch = "amd64"
				f.verify.Archive = filepath.Join(f.root, "shikigami-v1.2.3-linux-amd64.tar.gz")
			case "wrong-basename":
				f.verify.Archive = filepath.Join(f.root, "renamed.tar.gz")
			}
			result, err := Verify(context.Background(), f.verify)
			if err == nil || result != (Verified{}) {
				t.Fatal("invalid release accepted")
			}
			absent(t, f.verify.Output)
		})
	}
}

type actionContext struct {
	context.Context
	action func()
}

func (c actionContext) Err() error {
	c.action()
	return c.Context.Err()
}

func TestCancellationCleansOnlyNewOutputs(t *testing.T) {
	t.Run("before-start", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := Sign(ctx, f.sign); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := Verify(ctx, f.verify); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		absent(t, f.sign.Signature)
		absent(t, f.verify.Output)
	})
	t.Run("after-signature-created", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		observed := false
		wrapped := actionContext{Context: ctx, action: func() {
			if _, err := os.Stat(f.sign.Signature); err == nil {
				observed = true
				cancel()
			}
		}}
		if err := Sign(wrapped, f.sign); !errors.Is(err, context.Canceled) || !observed {
			t.Fatal("signing did not cancel after creating its output", err)
		}
		absent(t, f.sign.Signature)
	})
	t.Run("during-copy", func(t *testing.T) {
		f := newFixture(t)
		f.data = bytes.Repeat([]byte("streaming"), 1<<17)
		writeFile(t, f.sign.Archive, f.data, 0600)
		f.signed(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		observed := false
		wrapped := actionContext{Context: ctx, action: func() {
			info, err := os.Stat(filepath.Join(f.verify.Output, filepath.Base(f.sign.Archive)))
			if err == nil && info.Size() > 0 {
				observed = true
				cancel()
			}
		}}
		result, err := Verify(wrapped, f.verify)
		if !errors.Is(err, context.Canceled) || !observed || result != (Verified{}) {
			t.Fatal("verification did not cancel during copy", err)
		}
		absent(t, f.verify.Output)
		if !bytes.Equal(readFile(t, f.sign.Archive), f.data) {
			t.Fatal("cancelled verification changed original archive")
		}
	})
}

func TestInvalidOptionsAndPins(t *testing.T) {
	for _, value := range []string{"", "../escape", "a/b", "a\\b", strings.Repeat("v", 81), "_prefix", "v1\n"} {
		t.Run("version-"+value, func(t *testing.T) {
			f := newFixture(t)
			f.sign.Version, f.verify.Version = value, value
			if err := Sign(context.Background(), f.sign); err == nil {
				t.Fatal("invalid version signed")
			}
			if _, err := Verify(context.Background(), f.verify); err == nil {
				t.Fatal("invalid expected version accepted")
			}
			absent(t, f.sign.Signature)
			absent(t, f.verify.Output)
		})
	}
	for _, value := range []string{"", strings.Repeat("A", 64), strings.Repeat("0", 63), strings.Repeat("0", 64) + "\n\n", strings.Repeat("0", 64) + "\r\n", strings.Repeat("x", 64)} {
		t.Run("pin", func(t *testing.T) {
			f := newFixture(t)
			writeFile(t, f.sign.ChecksumsPin, []byte(value), 0600)
			if err := Sign(context.Background(), f.sign); err == nil {
				t.Fatal("invalid pin signed")
			}
			absent(t, f.sign.Signature)
		})
	}
	for _, platform := range [][2]string{{"windows", "arm64"}, {"linux", "386"}, {"", ""}} {
		if _, err := archiveName("v1", platform[0], platform[1]); err == nil {
			t.Fatal("unsupported platform accepted")
		}
	}
}
