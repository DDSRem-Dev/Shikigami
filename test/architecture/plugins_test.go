package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/DDSRem-Dev/Shikigami"

// Keep the ownership boundary in docs/07 executable: business I/O lives in
// standalone plugins; hosts depend on their public SDK contracts, not workers.
func TestPluginPlacementAndDependencyBoundary(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	for _, name := range []string{"sync", "port"} {
		if _, err := os.Stat(filepath.Join(root, "plugins", name, "main.go")); err != nil {
			t.Errorf("plugins/%s must own its executable entrypoint: %v", name, err)
		}
		for _, legacy := range []string{filepath.Join("cmd", "shiki-plugin-"+name), filepath.Join("internal", name+"plugin")} {
			if _, err := os.Stat(filepath.Join(root, legacy)); !os.IsNotExist(err) {
				t.Errorf("plugin implementation must not live in %s", legacy)
			}
		}
		imports(t, filepath.Join(root, "plugins", name), func(file, dependency string) {
			if !strings.HasPrefix(dependency, module+"/") {
				return
			}
			allowed := dependency == module+"/pkg/plugin" || strings.HasPrefix(dependency, module+"/pkg/plugin/") || dependency == module+"/pkg/protocol" || strings.HasPrefix(dependency, module+"/pkg/protocol/") || strings.HasPrefix(dependency, module+"/plugins/"+name+"/")
			if !allowed {
				t.Errorf("%s imports non-SDK host dependency %s", file, dependency)
			}
		})
	}
	imports(t, filepath.Join(root, "pkg", "plugin"), func(file, dependency string) {
		if strings.HasPrefix(dependency, module+"/internal/") {
			t.Errorf("standalone SDK %s imports host implementation %s", file, dependency)
		}
	})
	for _, directory := range []string{"internal", "cmd"} {
		imports(t, filepath.Join(root, directory), func(file, dependency string) {
			if strings.HasPrefix(dependency, module+"/plugins/") {
				t.Errorf("host %s imports plugin implementation %s; use SDK contract and subprocess", file, dependency)
			}
		})
	}
	imports(t, filepath.Join(root, "cmd", "shiki"), func(file, dependency string) {
		if strings.HasPrefix(dependency, module+"/internal/") && dependency != module+"/internal/client" && !strings.HasPrefix(dependency, module+"/internal/client/") {
			t.Errorf("CLI %s bypasses the frontend client boundary through %s", file, dependency)
		}
	})
}
func imports(t *testing.T, root string, check func(string, string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range parsed.Imports {
			dependency, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			check(name, dependency)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
