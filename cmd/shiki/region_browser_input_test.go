package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/proto"
)

func TestRegionBrowserInputErrorsPrecedeIPC(t *testing.T) {
	invalidRegion := filepath.Join(t.TempDir(), "region.json")
	if err := os.WriteFile(invalidRegion, []byte(`{"country":"US"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"region", "plan"}, "--file"},
		{[]string{"region", "plan", "--file", invalidRegion}, "US requires an explicit"},
		{[]string{"region", "apply", "plan_cli", "--yes"}, "--expected-revision"},
		{[]string{"region", "apply", "\n", "--expected-revision", "host-v1", "--yes"}, "plan ID"},
		{[]string{"browser", "profile", "create", "new_profile", "--yes"}, "--name"},
		{[]string{"browser", "profile", "update", "profile_cli", "--yes"}, "requires --name"},
		{[]string{"browser", "profile", "update", "profile_cli", "--name=", "--yes"}, "name must not be empty"},
		{[]string{"browser", "profile", "create", "new_profile", "--name", "Name", "--file", invalidRegion, "--inherit-region", "--yes"}, "mutually exclusive"},
		{[]string{"browser", "profile", "boot", "profile_cli", "--enabled=false", "--replace-boot", "--yes"}, "only with --enabled=true"},
		{[]string{"browser", "start", "../profile", "--yes"}, "not a path"},
		{[]string{"browser", "open", "profile\tcontrol"}, "control characters"},
		{[]string{"browser", "files", "list", "profile_cli", "--area", "/tmp"}, "uploads or downloads"},
		{[]string{"browser", "files", "upload", "profile_cli", "local", "--name", "../secret", "--yes"}, "single flat name"},
		{[]string{"browser", "files", "download", "profile_cli", "../secret", "--output", "local", "--yes"}, "single flat name"},
		{[]string{"browser", "files", "download", "profile_cli", "download.txt", "--yes"}, "explicit --output"},
		{[]string{"region", "diagnose", "--allow-external"}, "requires --providers"},
		{[]string{"browser", "diagnose", "profile_cli", "--providers", "unused.json"}, "requires --allow-external"},
	} {
		t.Run(strings.Join(tc.args[:2], "/")+"/"+tc.want, func(t *testing.T) {
			var output, diagnostics bytes.Buffer
			root := newRoot(nil, &output, &diagnostics)
			root.SetArgs(append([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}, tc.args...))
			err := root.ExecuteContext(t.Context())
			if err == nil || !strings.Contains(err.Error(), tc.want) || output.Len() != 0 {
				t.Fatalf("input was not rejected before IPC: args=%v output=%q err=%v", tc.args, output.String(), err)
			}
		})
	}
}

func TestRegionConfigurationRejectsUSGuessingAndUnknownJSON(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"country":"US"}`, "US requires an explicit"},
		{`{"country":"US","timezone":"America/New_York","locale":"en_US.UTF-8"}`, "languages"},
		{`{"country":"US","timezone":"America/New_York","locale":"en_US.UTF-8","languages":[""]}`, "each language"},
		{`{"country":"US","timezone":"not/an/IANA/zone","locale":"en_US.UTF-8","languages":["en_US"]}`, "IANA timezone"},
		{`{"country":"GB"}`, "JP or US"},
		{`{"country":"JP","unknown":"not silently ignored"}`, "invalid RegionConfig JSON"},
		{`{"country":"JP","country":"US"}`, "invalid RegionConfig JSON"},
	} {
		file := filepath.Join(t.TempDir(), "region.json")
		if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readRegionConfig(file); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("configuration %s: %v", tc.body, err)
		}
	}
}

func TestDiagnosticProviderConsentAndTypedInput(t *testing.T) {
	request, err := readDiagnosticProviders("", false)
	if err != nil || request.AllowExternal || request.AllowWebrtc || len(request.Providers) != 0 {
		t.Fatalf("implicit external probing: %v %v", request, err)
	}
	valid := `{"providers":[{"id":"self-hosted","kind":"ip-echo","url":"https://diagnostic.example/ip","ip_family":"ipv4"}]}`
	for _, tc := range []struct{ body, want string }{
		{valid, ""},
		{`{"providers":[]}`, "at least one"},
		{`{"server_id":"other","providers":[{"id":"p","kind":"ip-echo","url":"https://example.com"}]}`, "only providers"},
		{`{"allow_webrtc":true,"providers":[{"id":"p","kind":"ip-echo","url":"https://example.com"}]}`, "only providers"},
		{`{"providers":[{"id":"p","kind":"ip-echo","url":"http://example.com"}]}`, "HTTPS"},
		{`{"providers":[{"id":"p","kind":"ip-echo","url":"https://user:password@example.com"}]}`, "HTTPS"},
		{`{"providers":[{"id":"p","kind":"ip-echo","url":"https://example.com/ip?token=secret"}]}`, "HTTPS"},
		{`{"providers":[{"id":"p","kind":"ip-echo","url":"https://example.com:8443/ip"}]}`, "HTTPS"},
		{`{"providers":[{"id":"p","kind":"ip-echo","url":"https://example.com","ip_family":"guess-country"}]}`, "ip_family"},
		{`{"providers":[{"id":"p","kind":"dns-observer","url":"https://example.com"}]}`, "dns_zone"},
		{`{"providers":[{"id":"p","kind":"arbitrary-script","url":"https://example.com"}]}`, "kind must be"},
		{`{"providers":[{"id":"p","kind":"ip-echo","url":"https://example.com"},{"id":"p","kind":"geo","url":"https://example.com"}]}`, "duplicate"},
		{`{"providers":[{"id":"p","kind":"ip-echo","url":"https://example.com","token":"never accepted"}]}`, "invalid diagnostic providers JSON"},
	} {
		file := filepath.Join(t.TempDir(), "providers.json")
		if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		request, err := readDiagnosticProviders(file, true)
		if tc.want != "" {
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("providers %s: %v", tc.body, err)
			}
		} else if err != nil || !request.AllowExternal || len(request.Providers) != 1 || request.Providers[0].IpFamily != "ipv4" {
			t.Fatalf("explicit providers not preserved: %v %v", request, err)
		}
		if _, err := readDiagnosticProviders(file, false); err == nil || !strings.Contains(err.Error(), "--allow-external") {
			t.Fatalf("provider file implied consent: %v", err)
		}
	}
}

func TestViewerURLValidationWithholdsBootstrapCredentials(t *testing.T) {
	for _, value := range []string{
		"https://example.com/bootstrap?token=must-not-appear",
		"http://192.0.2.1:8080/bootstrap?token=must-not-appear",
		"http://user:must-not-appear@127.0.0.1:8080/",
		"http://127.0.0.1/bootstrap?token=must-not-appear",
		"http://127.0.0.1:70000/bootstrap?token=must-not-appear",
	} {
		err := validateCLIViewerURL(value)
		if err == nil || strings.Contains(err.Error(), "must-not-appear") {
			t.Fatalf("invalid bootstrap URL was accepted or leaked: %v", err)
		}
	}
	for _, value := range []string{"http://127.0.0.1:8080/bootstrap#secret", "http://[::1]:8080/bootstrap#secret", "http://localhost:8080/bootstrap#secret"} {
		if err := validateCLIViewerURL(value); err != nil {
			t.Fatalf("valid loopback URL rejected: %v", err)
		}
	}
}

func TestBrowserDownloadRejectsTruncationCorruptionAndScopeChanges(t *testing.T) {
	request := &epb.BrowserFileRequest{ServerId: "server", ProfileId: "profile", Area: "downloads", Name: "file.bin"}
	data := []byte("verified")
	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])
	base := []*epb.BrowserFileChunk{{TotalSize: int64(len(data)), Sha256: checksum}, {Data: data}, {Finish: true, Sha256: checksum}}
	for _, tc := range []struct {
		name  string
		alter func([]*epb.BrowserFileChunk) []*epb.BrowserFileChunk
		want  string
	}{
		{"complete", func(chunks []*epb.BrowserFileChunk) []*epb.BrowserFileChunk { return chunks }, ""},
		{"truncated", func(chunks []*epb.BrowserFileChunk) []*epb.BrowserFileChunk { return chunks[:2] }, "finish frame"},
		{"corrupt", func(chunks []*epb.BrowserFileChunk) []*epb.BrowserFileChunk {
			chunks[1].Data = []byte("changed!")
			return chunks
		}, "verification failed"},
		{"wrong profile", func(chunks []*epb.BrowserFileChunk) []*epb.BrowserFileChunk {
			chunks[1].ProfileId = "other"
			return chunks
		}, "does not match"},
		{"oversize", func(chunks []*epb.BrowserFileChunk) []*epb.BrowserFileChunk {
			chunks[1].Data = bytes.Repeat([]byte("x"), browserFileChunkSize+1)
			return chunks
		}, "bounded frame limit"},
		{"after finish", func(chunks []*epb.BrowserFileChunk) []*epb.BrowserFileChunk {
			return append(chunks, &epb.BrowserFileChunk{})
		}, "frame sequence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunks := make([]*epb.BrowserFileChunk, len(base))
			for i, chunk := range base {
				chunks[i] = proto.Clone(chunk).(*epb.BrowserFileChunk)
			}
			chunks = tc.alter(chunks)
			var output bytes.Buffer
			i := 0
			receive := func() (*epb.BrowserFileChunk, error) {
				if i == len(chunks) {
					return nil, io.EOF
				}
				chunk := chunks[i]
				i++
				return chunk, nil
			}
			result, err := receiveCLIBrowserFile(request, receive, &output)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("invalid download accepted: %v", err)
				}
			} else if err != nil || result.Sha256 != checksum || !bytes.Equal(output.Bytes(), data) {
				t.Fatalf("valid download: %v %v", result, err)
			}
		})
	}
	_, err := receiveCLIBrowserFile(request, func() (*epb.BrowserFileChunk, error) { return nil, os.ErrClosed }, io.Discard)
	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("cancellation/stream error was suppressed: %v", err)
	}
}
