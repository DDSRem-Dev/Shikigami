package reconcile

import (
	"errors"
	"strings"
	"testing"
)

func TestIgnoreDefaultsWithoutFile(t *testing.T) {
	matcher, err := NewIgnore(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".git/config", "a/node_modules/package/a.js", ".ssh/id_ed25519", ".aws/credentials", ".claude/settings.json", ".env", "sub/.env.production", ".shiki-sync-history/x", "../escape", "/absolute"} {
		if !matcher.Ignored(name, false) {
			t.Errorf("must ignore %q", name)
		}
	}
	for _, name := range []string{"main.go", "src/a.go", ".env.example", "nested/.env.sample", "notnode_modules/a.js"} {
		if matcher.Ignored(name, false) {
			t.Errorf("unexpected ignore %q", name)
		}
	}
}
func TestIgnoreAnchorsNegationDirectoriesAndDoubleStar(t *testing.T) {
	matcher, err := NewIgnore(strings.NewReader("# comment\n/root.txt\n*.log\n!important.log\nbuild/\n!build/important.txt\ncache/**/secret?.[ch]\n\\#literal\n\\!literal\ntrailing   \n"))
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]bool{
		"root.txt": true, "sub/root.txt": false, "a/debug.log": true, "a/important.log": false,
		"build/output": true, "build/important.txt": true, "build": false, "cache/secret1.c": true,
		"cache/a/b/secret2.h": true, "cache/a/secret22.h": false, "else/cache/secret1.c": false,
		"#literal": true, "!literal": true, "trailing": true,
	}
	for name, want := range tests {
		if got := matcher.Ignored(name, false); got != want {
			t.Errorf("%q = %v, want %v", name, got, want)
		}
	}
	if !matcher.Ignored("build", true) {
		t.Fatal("directory-only rule failed")
	}
}
func TestIgnoreErrorsAndPrivateNamespace(t *testing.T) {
	for _, input := range []string{"bad[", strings.Repeat("x", 70<<10), strings.Repeat("# comment\n", 120000)} {
		if _, err := NewIgnore(strings.NewReader(input)); err == nil {
			t.Fatal("accepted malformed/oversized input")
		}
	}
	if _, err := NewIgnore(errorReader{}); err == nil {
		t.Fatal("reader error discarded")
	}
	matcher, err := NewIgnore(strings.NewReader("!.shiki-sync-history/\n!**\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !matcher.Ignored(".shiki-sync-history/file", false) {
		t.Fatal("internal files can be unignored")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("ignore read failed") }
