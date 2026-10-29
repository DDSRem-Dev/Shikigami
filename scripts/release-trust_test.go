//go:build ignore

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run explicitly with the build-ignored tool:
// go test ./scripts/release-trust.go ./scripts/release-trust_test.go
func TestArgumentsAndHelpDoNotAccessFiles(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"sign", "--help"}, {"verify", "--help"}, {"fetch", "--help"}} {
		var out, diagnostic bytes.Buffer
		if code := run(t.Context(), args, &out, &diagnostic); code != 0 {
			t.Fatalf("%v: code=%d diagnostic=%s", args, code, diagnostic.String())
		}
	}
	for _, args := range [][]string{{"unknown"}, {"help", "extra"}, {"sign"}, {"verify"}, {"fetch"}, {"verify", "--unknown"}, {"fetch", "--archive", "local"}, {"verify", "--archive-url", "https://example.invalid"}, {"sign", "positional"}} {
		var out, diagnostic bytes.Buffer
		if code := run(t.Context(), args, &out, &diagnostic); code != 2 || out.Len() != 0 && args[0] != "help" {
			t.Fatalf("%v: code=%d out=%s diagnostic=%s", args, code, out.String(), diagnostic.String())
		}
	}
}

func TestFetchCLIRejectsBeforeDownload(t *testing.T) {
	for _, address := range []string{
		"http://example.invalid/archive?secret-token",
		"https://user:secret-token@example.invalid/archive",
		"https://example.invalid/archive#secret-token",
	} {
		t.Run(address[:strings.Index(address, ":")]+"-rejected", func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "verified")
			args := []string{"fetch", "--archive-url", address,
				"--signature-url", "https://example.invalid/signature",
				"--public-key", filepath.Join(t.TempDir(), "absent.pem"),
				"--output", output, "--version", "v0.0.0-test", "--os", "linux", "--arch", "arm64"}
			var out, diagnostic bytes.Buffer
			if code := run(t.Context(), args, &out, &diagnostic); code != 1 || out.Len() != 0 {
				t.Fatalf("fetch code=%d out=%s diagnostic=%s", code, out.String(), diagnostic.String())
			}
			if strings.Contains(diagnostic.String(), "secret-token") || !strings.HasPrefix(diagnostic.String(), "fetch:") || !strings.Contains(diagnostic.String(), "HTTPS URLs") {
				t.Fatalf("unsafe or misclassified diagnostic: %s", diagnostic.String())
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("rejected fetch created output: %v", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			out.Reset()
			diagnostic.Reset()
			if code := run(ctx, args, &out, &diagnostic); code != 1 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "context canceled") {
				t.Fatalf("cancelled fetch code=%d out=%s diagnostic=%s", code, out.String(), diagnostic.String())
			}
		})
	}
}

func TestSignedArchiveCLIWorkflow(t *testing.T) {
	dir := t.TempDir()
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	privatePath := write("private.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
	publicPath := write("public.pem", pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	sums := []byte("fixture-checksums\n")
	pin := sha256.Sum256(sums)
	pinPath := write("archive.checksums-pin", []byte(hex.EncodeToString(pin[:])+"\n"))
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "SHA256SUMS", Mode: 0600, Size: int64(len(sums))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(sums); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archivePath := write("shikigami-v0.0.0-test-linux-arm64.tar.gz", archive.Bytes())
	signaturePath := filepath.Join(dir, "archive.sig.json")
	target := []string{"--version", "v0.0.0-test", "--os", "linux", "--arch", "arm64"}
	signArgs := append([]string{"sign", "--archive", archivePath, "--signature", signaturePath, "--private-key", privatePath, "--checksums-pin", pinPath}, target...)
	var out, diagnostic bytes.Buffer
	if code := run(t.Context(), signArgs, &out, &diagnostic); code != 0 {
		t.Fatalf("sign code=%d: %s", code, diagnostic.String())
	}
	output := filepath.Join(dir, "verified")
	verifyArgs := append([]string{"verify", "--archive", archivePath, "--signature", signaturePath, "--public-key", publicPath, "--output", output}, target...)
	out.Reset()
	diagnostic.Reset()
	if code := run(t.Context(), verifyArgs, &out, &diagnostic); code != 0 {
		t.Fatalf("verify code=%d: %s", code, diagnostic.String())
	}
	var result struct {
		Archive         string `json:"archive"`
		ChecksumsSHA256 string `json:"checksums_sha256"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ChecksumsSHA256 != hex.EncodeToString(pin[:]) || filepath.Dir(result.Archive) != output {
		t.Fatalf("unexpected verified result: %s", out.String())
	}
	copy, err := os.ReadFile(result.Archive)
	if err != nil || !bytes.Equal(copy, archive.Bytes()) {
		t.Fatalf("verified archive differs: %v", err)
	}
	t.Run("HTTPS-fetch-command", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var path string
			switch r.URL.Path {
			case "/signature":
				path = signaturePath
			case "/archive":
				path = archivePath
			default:
				http.NotFound(w, r)
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Error(err)
				http.Error(w, "fixture unavailable", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(data)
		}))
		defer server.Close()
		rootsPath := write("fixture-roots.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
		fetchOutput := filepath.Join(dir, "fetched")
		fetchArgs := append([]string{"-test.run=^TestFetchCLIChild$", "--", "fetch",
			"--archive-url", server.URL + "/archive?fixture-secret",
			"--signature-url", server.URL + "/signature?fixture-secret",
			"--public-key", publicPath, "--output", fetchOutput}, target...)
		cmd := exec.CommandContext(t.Context(), os.Args[0], fetchArgs...)
		// Test-only fallback roots live in a separate process. The production
		// Fetch transport still uses its standard system-trust configuration.
		cmd.Env = append(os.Environ(), "SHIKI_TEST_FETCH_ROOTS="+rootsPath, "GODEBUG=x509usefallbackroots=1")
		var fetchedJSON, childErrors bytes.Buffer
		cmd.Stdout, cmd.Stderr = &fetchedJSON, &childErrors
		if err := cmd.Run(); err != nil {
			t.Fatalf("fetch command: %v stdout=%s stderr=%s", err, fetchedJSON.String(), childErrors.String())
		}
		if strings.Contains(fetchedJSON.String()+childErrors.String(), "fixture-secret") {
			t.Fatal("URL query leaked")
		}
		var fetched struct {
			Archive         string `json:"archive"`
			ChecksumsSHA256 string `json:"checksums_sha256"`
		}
		if err := json.Unmarshal(fetchedJSON.Bytes(), &fetched); err != nil || fetched.ChecksumsSHA256 != result.ChecksumsSHA256 || filepath.Dir(fetched.Archive) != fetchOutput {
			t.Fatalf("invalid fetch result: %v %s", err, fetchedJSON.String())
		}
		data, err := os.ReadFile(fetched.Archive)
		if err != nil || !bytes.Equal(data, archive.Bytes()) {
			t.Fatalf("fetch bytes differ: %v", err)
		}
		if _, err := os.Stat(filepath.Join(fetchOutput, "receipt.json")); err != nil {
			t.Fatal(err)
		}
	})
	// stdout delivery is outside the completed signing/verification transaction.
	failedSign := append([]string(nil), signArgs...)
	failedSign[4] = filepath.Join(dir, "stdout-error.sig.json")
	if code := run(t.Context(), failedSign, failingOutput{}, &diagnostic); code != 1 {
		t.Fatalf("sign stdout error code=%d", code)
	}
	if info, err := os.Stat(failedSign[4]); err != nil || info.Size() == 0 {
		t.Fatalf("completed signature missing after stdout error: %v", err)
	}
	failedVerify := append([]string(nil), verifyArgs...)
	failedVerify[4] = failedSign[4]
	failedVerify[8] = filepath.Join(dir, "stdout-error-verified")
	if code := run(t.Context(), failedVerify, failingOutput{}, &diagnostic); code != 1 {
		t.Fatalf("verify stdout error code=%d", code)
	}
	if _, err := os.Stat(filepath.Join(failedVerify[8], "receipt.json")); err != nil {
		t.Fatalf("completed receipt missing after stdout error: %v", err)
	}
	retained, err := os.ReadFile(filepath.Join(failedVerify[8], filepath.Base(archivePath)))
	if err != nil || !bytes.Equal(retained, archive.Bytes()) {
		t.Fatalf("authenticated copy missing after stdout error: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	verifyArgs[8] = filepath.Join(dir, "tampered-output")
	out.Reset()
	diagnostic.Reset()
	if code := run(t.Context(), verifyArgs, &out, &diagnostic); code != 1 || out.Len() != 0 {
		t.Fatalf("tampered archive: code=%d out=%s diagnostic=%s", code, out.String(), diagnostic.String())
	}
	if _, err := os.Lstat(verifyArgs[8]); !os.IsNotExist(err) {
		t.Fatalf("failed verification left output: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	signArgs[4] = filepath.Join(dir, "cancelled.sig.json")
	out.Reset()
	diagnostic.Reset()
	if code := run(ctx, signArgs, &out, &diagnostic); code != 1 || out.Len() != 0 {
		t.Fatalf("cancelled sign: code=%d out=%s diagnostic=%s", code, out.String(), diagnostic.String())
	}
}

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestFetchCLIChild(t *testing.T) {
	path := os.Getenv("SHIKI_TEST_FETCH_ROOTS")
	if path == "" {
		return // Only the parent HTTPS workflow invokes this subprocess entrypoint.
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		t.Fatal("invalid fixture roots")
	}
	x509.SetFallbackRoots(roots)
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(run(context.Background(), os.Args[i+1:], os.Stdout, os.Stderr))
		}
	}
	t.Fatal("missing subprocess command")
}
