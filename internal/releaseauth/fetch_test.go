package releaseauth

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func fetchFixture(t *testing.T, handler func(*fixture, http.ResponseWriter, *http.Request)) (*fixture, *http.Client, FetchOptions) {
	t.Helper()
	f := newFixture(t)
	f.signed(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler(f, w, r) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client := fetchClient(roots)
	t.Cleanup(client.CloseIdleConnections)
	opts := FetchOptions{ArchiveURL: server.URL + "/release?token=secret-query", SignatureURL: server.URL + "/signature?token=secret-query",
		PublicKey: f.verify.PublicKey, Output: f.verify.Output, Version: f.sign.Version, OS: f.sign.OS, Arch: f.sign.Arch}
	return f, client, opts
}

func TestFetchAuthenticatesBeforeArchiveAndCreatesPrivateReceipt(t *testing.T) {
	var order []string
	var orderMu sync.Mutex
	var gotProtocol atomic.Int32
	f, client, opts := fetchFixture(t, func(f *fixture, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "identity")
		orderMu.Lock()
		order = append(order, r.URL.Path)
		orderMu.Unlock()
		gotProtocol.Store(int32(r.ProtoMajor))
		if r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected credentials or compression negotiation")
		}
		if r.URL.Path == "/signature" {
			_, _ = w.Write(readFile(t, f.sign.Signature))
			return
		}
		_, _ = w.Write(f.data)
	})
	result, err := fetch(context.Background(), opts, client)
	if err != nil {
		t.Fatal(err)
	}
	orderMu.Lock()
	observed := fmt.Sprint(order)
	orderMu.Unlock()
	if observed != "[/signature /release]" || gotProtocol.Load() != 2 {
		t.Fatal("requests were not ordered over TLS HTTP2", observed, gotProtocol.Load())
	}
	if !bytes.Equal(readFile(t, result.Archive), f.data) || filepath.Base(result.Archive) != filepath.Base(f.sign.Archive) || result.ChecksumsSHA256 != f.pin {
		t.Fatal("authenticated output differs from signed input")
	}
	mode(t, opts.Output, 0700)
	for _, name := range []string{result.Archive, filepath.Join(opts.Output, "checksums-pin"), filepath.Join(opts.Output, "receipt.json")} {
		mode(t, name, 0600)
	}
	var receipt Verified
	if err = json.Unmarshal(readFile(t, filepath.Join(opts.Output, "receipt.json")), &receipt); err != nil || receipt != result {
		t.Fatal("receipt differs", err)
	}
	entries, err := os.ReadDir(opts.Output)
	if err != nil || len(entries) != 3 {
		t.Fatal("unexpected extraction or temporary files", err)
	}
	if _, err = fetch(context.Background(), opts, client); err == nil || !bytes.Equal(readFile(t, result.Archive), f.data) {
		t.Fatal("existing authenticated output overwritten", err)
	}
}

func TestFetchRejectsUnauthenticatedMetadataBeforeArchive(t *testing.T) {
	for _, test := range []string{"wrong-key", "invalid-signature", "version", "os", "arch", "signature-overflow", "signature-declared-overflow", "signature-status", "signature-redirect", "signature-encoding"} {
		t.Run(test, func(t *testing.T) {
			var archiveCalls atomic.Int32
			_, client, opts := fetchFixture(t, func(f *fixture, w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/signature" {
					archiveCalls.Add(1)
					_, _ = w.Write(f.data)
					return
				}
				switch test {
				case "invalid-signature":
					_, _ = w.Write([]byte(`{"format":1,"payload":"e30=","signature":"bad"}`))
				case "signature-overflow":
					w.(http.Flusher).Flush()
					_, _ = w.Write(bytes.Repeat([]byte(" "), MaxMetadataBytes+1))
				case "signature-declared-overflow":
					w.Header().Set("Content-Length", fmt.Sprint(MaxMetadataBytes+1))
				case "signature-status":
					http.Error(w, "secret-query", http.StatusUnauthorized)
				case "signature-redirect":
					http.Redirect(w, r, "/release?redirect=secret-query", http.StatusFound)
				case "signature-encoding":
					w.Header().Set("Content-Encoding", "gzip")
					_, _ = w.Write(readFile(t, f.sign.Signature))
				default:
					_, _ = w.Write(readFile(t, f.sign.Signature))
				}
			})
			switch test {
			case "wrong-key":
				opts.PublicKey = newFixture(t).verify.PublicKey
			case "version":
				opts.Version = "v9.9.9"
			case "os":
				opts.OS = "darwin"
			case "arch":
				opts.Arch = "amd64"
			}
			result, err := fetch(context.Background(), opts, client)
			if err == nil || result != (Verified{}) || archiveCalls.Load() != 0 || strings.Contains(err.Error(), "secret-query") {
				t.Fatalf("untrusted metadata admitted/archive fetched/error leaked: result=%+v error=%v calls=%d", result, err, archiveCalls.Load())
			}
			absent(t, opts.Output)
		})
	}
}

func TestFetchArchiveFailuresCleanOwnedOutput(t *testing.T) {
	for _, test := range []string{"changed-byte", "truncated", "appended", "declared-size", "redirect", "status", "encoding", "multiple-encodings", "oversized-header"} {
		t.Run(test, func(t *testing.T) {
			var redirects atomic.Int32
			_, client, opts := fetchFixture(t, func(f *fixture, w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/signature" {
					_, _ = w.Write(readFile(t, f.sign.Signature))
					return
				}
				if r.URL.Path == "/redirected" {
					redirects.Add(1)
				}
				switch test {
				case "changed-byte":
					data := append([]byte(nil), f.data...)
					data[0] ^= 1
					_, _ = w.Write(data)
				case "truncated":
					w.Header().Set("Content-Length", fmt.Sprint(len(f.data)))
					_, _ = w.Write(f.data[:len(f.data)-1])
				case "appended":
					w.(http.Flusher).Flush()
					_, _ = w.Write(append(append([]byte(nil), f.data...), 'x'))
				case "declared-size":
					w.Header().Set("Content-Length", fmt.Sprint(len(f.data)-1))
				case "redirect":
					http.Redirect(w, r, "/redirected?secret-query", http.StatusTemporaryRedirect)
				case "status":
					http.Error(w, "secret-query", http.StatusServiceUnavailable)
				case "encoding":
					w.Header().Set("Content-Encoding", "gzip")
				case "multiple-encodings":
					w.Header().Add("Content-Encoding", "identity")
					w.Header().Add("Content-Encoding", "gzip")
				case "oversized-header":
					w.Header().Set("X-Oversized", strings.Repeat("secret-query", maxHeaderBytes))
				}
			})
			result, err := fetch(context.Background(), opts, client)
			if err == nil || result != (Verified{}) || strings.Contains(err.Error(), "secret-query") || redirects.Load() != 0 {
				t.Fatal("failed download admitted or unsafe diagnostic", result, err)
			}
			absent(t, opts.Output)
		})
	}
}

func TestFetchPublicAPIUsesSystemTrustAndValidatesBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	_, client, opts := fetchFixture(t, func(f *fixture, w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	if _, err := Fetch(context.Background(), opts); err == nil || strings.Contains(err.Error(), "secret-query") || calls.Load() != 0 {
		t.Fatal("public Fetch trusted test certificate or leaked URL", err)
	}
	for _, endpoint := range []string{"", "http://example.invalid/release", "https://user:secret-query@example.invalid/release", "https://example.invalid/release#secret-query", "https://example.invalid/release#", "https:///release", "https:opaque", "https://example.invalid/%zz?secret-query"} {
		for _, field := range []string{"archive", "signature"} {
			t.Run(field+endpoint, func(t *testing.T) {
				bad := opts
				if field == "archive" {
					bad.ArchiveURL = endpoint
				} else {
					bad.SignatureURL = endpoint
				}
				if _, err := fetch(context.Background(), bad, client); err == nil || strings.Contains(err.Error(), "secret-query") || calls.Load() != 0 {
					t.Fatal("invalid URL reached network or leaked diagnostic", err)
				}
				absent(t, bad.Output)
			})
		}
	}
	for _, version := range []string{"", "latest", "LATEST", "../bad"} {
		bad := opts
		bad.Version = version
		if _, err := fetch(context.Background(), bad, client); err == nil || calls.Load() != 0 {
			t.Fatal("invalid/unfixed version reached network", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Fetch(ctx, opts); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatal("pre-canceled fetch reached network", err)
	}
	absent(t, opts.Output)
}
