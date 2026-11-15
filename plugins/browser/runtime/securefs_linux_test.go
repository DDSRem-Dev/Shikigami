//go:build linux

package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func TestAtomicJSONEnforcesEncodedReaderLimitBeforePublication(t *testing.T) {
	for _, maximum := range []int64{MaxControlBody, 8 << 20} {
		t.Run(strconv.FormatInt(maximum, 10)+"-bytes", func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chmod(directory, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "state.json")
			value := strings.Repeat("a", int(maximum)-2)
			if err := AtomicJSON(path, value, maximum); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil || int64(len(before)) != maximum {
				t.Fatalf("exact-boundary publication size=%d err=%v", len(before), err)
			}
			if err := AtomicJSON(path, value+"a", maximum); err == nil {
				t.Fatal("published an encoded value beyond its reader limit")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("oversized value replaced readable state: %v", err)
			}
			var decoded string
			if err := ReadJSON(path, &decoded, maximum); err != nil || decoded != value {
				t.Fatalf("boundary value cannot be read: %v", err)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != "state.json" {
				t.Fatalf("oversized write created temporary state: %v %#v", err, entries)
			}
		})
	}
}

func TestAtomicJSONRejectsEscapedLaunchRecordWithoutReplacingIt(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "launch.json")
	original := Spec{ProfileID: "original"}
	if err := AtomicJSON(path, original, MaxControlBody); err != nil {
		t.Fatal(err)
	}
	template := "https://resolver.example/dns-query?x=" + strings.Repeat("&", 2000)
	if len(template) > 2048 {
		t.Fatal("fixture exceeds the accepted raw template bound")
	}
	oversized := Spec{ProfileID: "oversized", Region: &epb.RegionConfig{Dns: &epb.DNSConfig{ChromeMode: "secure", ChromeTemplates: []string{template, template, template, template, template, template, template, template}}}}
	encoded, err := json.Marshal(oversized)
	if err != nil || len(encoded) <= MaxControlBody {
		t.Fatalf("fixture does not exceed the encoded launch bound: %d %v", len(encoded), err)
	}
	if err := AtomicJSON(path, oversized, MaxControlBody); err == nil {
		t.Fatal("published the escaped oversized launch record")
	}
	var restored Spec
	if err := ReadJSON(path, &restored, MaxControlBody); err != nil || restored.ProfileID != original.ProfileID || restored.Region != nil {
		t.Fatalf("previous launch record was not preserved: %#v %v", restored, err)
	}
}

func TestAtomicFileDistinguishesPublishedDirectorySyncFailure(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	newData := []byte(`{"revision":"committed"}`)
	syncCalls := 0
	err := atomicFileWithSync(path, newData, func(parent string) error {
		syncCalls++
		if parent != directory {
			t.Fatalf("synchronized another directory: %q", parent)
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, newData) {
			t.Fatalf("sync ran before publication: %q %v", data, err)
		}
		return syscall.EIO
	})
	var published *PublishedError
	if !errors.As(err, &published) || !errors.Is(err, syscall.EIO) || syncCalls != 1 {
		t.Fatalf("post-publication failure lost its classification: %v calls=%d", err, syncCalls)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(data, newData) {
		t.Fatalf("published file was rolled back: %q %v", data, readErr)
	}
	blocked := filepath.Join(directory, "blocked.json")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	err = atomicFileWithSync(blocked, []byte("not published"), func(string) error {
		t.Fatal("synchronized a failed publication")
		return nil
	})
	if err == nil || errors.As(err, &published) {
		t.Fatalf("pre-publication failure marked committed: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatalf("failed publication leaked temporary files: %#v %v", entries, err)
	}
}
