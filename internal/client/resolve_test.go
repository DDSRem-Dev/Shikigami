package client

import (
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"os"
	"path/filepath"
	"testing"
)

func TestMappingCanonicalSpecificity(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "project", "nested")
	cwd := filepath.Join(nested, "work")
	if e := os.MkdirAll(cwd, 0700); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(root, "a-very-long-symlink-name-that-must-not-affect-specificity")
	if e := os.Symlink(filepath.Join(root, "project"), link); e != nil {
		t.Fatal(e)
	}
	parent := &ppb.Mapping{Id: "parent", LocalRoot: link}
	child := &ppb.Mapping{Id: "child", LocalRoot: nested}
	for _, mappings := range [][]*ppb.Mapping{{parent, child}, {child, parent}} {
		result, rel, e := MappingForDirectory(cwd, mappings)
		if e != nil || result != child || rel != "work" {
			t.Fatalf("result=%v rel=%q error=%v", result, rel, e)
		}
	}
	other := filepath.Join(root, "project-old")
	if e := os.MkdirAll(other, 0700); e != nil {
		t.Fatal(e)
	}
	result, _, e := MappingForDirectory(other, []*ppb.Mapping{parent})
	if e != nil || result != nil {
		t.Fatalf("component boundary matched: %v %v", result, e)
	}
}
func TestConfigPathHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for input, want := range map[string]string{"~": home, "~/config.yaml": filepath.Join(home, "config.yaml")} {
		got, e := ConfigPath(input)
		if e != nil || got != want {
			t.Fatalf("%q -> %q %v", input, got, e)
		}
	}
	if _, e := ConfigPath("~someone/config"); e == nil {
		t.Fatal("other user expansion accepted")
	}
}
