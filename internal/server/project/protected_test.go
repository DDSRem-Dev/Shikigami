package project

import (
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func protectedBrowserState(t *testing.T, f fixture) string {
	t.Helper()
	chrome := filepath.Join(f.stateDir, "plugin-data", "browser", "profiles", "profile", "chrome")
	if err := os.MkdirAll(chrome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chrome, "Cookies"), []byte("private browser state"), 0600); err != nil {
		t.Fatal(err)
	}
	return chrome
}

func TestProtectedStateRejectsNewExistingAndAncestorProjects(t *testing.T) {
	f := setup(t)
	chrome := protectedBrowserState(t, f)
	alias := filepath.Join(f.base, "state-alias")
	if err := os.Symlink(f.stateDir, alias); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(f.stateDir, "plugin-data", "browser", "profiles", "new", "chrome")
	for _, test := range []struct{ name, path string }{
		{"existing-profile", chrome},
		{"state-root", f.stateDir},
		{"profile-parent", filepath.Dir(chrome)},
		{"state-ancestor", filepath.Dir(f.stateDir)},
		{"missing-profile", missing},
		{"existing-alias", filepath.Join(alias, "plugin-data", "browser", "profiles", "profile", "chrome")},
		{"missing-alias", filepath.Join(alias, "plugin-data", "browser", "profiles", "new", "chrome")},
		{"ancestor-traversal", chrome + string(filepath.Separator) + ".." + string(filepath.Separator) + "chrome"},
	} {
		t.Run(test.name, func(t *testing.T) {
			project, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: test.name, Path: test.path, RequestId: test.name})
			if connect.CodeOf(err) != connect.CodePermissionDenied || project != nil {
				t.Fatalf("protected association admitted: %v %v", project, err)
			}
		})
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("rejected association created browser directories", err)
	}
	// Directory-boundary matching must not over-block an ordinary sibling.
	sibling := f.stateDir + "-public"
	if err := os.Mkdir(sibling, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "public-sibling", Path: sibling, RequestId: "public-sibling"}); err != nil {
		t.Fatal("textual state prefix blocked an unrelated directory", err)
	}
}

func TestProtectedLegacyAssociationsCannotResolveOrTraverse(t *testing.T) {
	for _, root := range []string{"state", "browser", "chrome", "ancestor"} {
		t.Run(root, func(t *testing.T) {
			f := setup(t)
			chrome := protectedBrowserState(t, f)
			p := create(t, f, "legacy")
			p.Path = map[string]string{"state": f.stateDir, "browser": filepath.Join(f.stateDir, "plugin-data", "browser"), "chrome": chrome, "ancestor": filepath.Dir(f.stateDir)}[root]
			if err := f.store.Update(func(state *storage.Snapshot) error { return state.Put(storage.ProjectsBucket, p.Id, p) }); err != nil {
				t.Fatal(err)
			}
			if project, err := f.manager.Get(t.Context(), &pb.GetProjectRequest{Id: p.Id}); connect.CodeOf(err) != connect.CodePermissionDenied || project != nil {
				t.Fatalf("legacy protected project resolved: %v %v", project, err)
			}
			relative, err := filepath.Rel(p.Path, chrome)
			if err != nil {
				t.Fatal(err)
			}
			if relative == "." {
				relative = ""
			}
			for _, path := range []string{"", filepath.ToSlash(relative)} {
				if choices, err := f.manager.ListDirectories(t.Context(), &pb.ListProjectDirectoriesRequest{ProjectId: p.Id, Path: path}); connect.CodeOf(err) != connect.CodePermissionDenied || choices != nil {
					t.Fatalf("legacy protected traversal %q admitted: %v %v", path, choices, err)
				}
			}
			if _, err := f.manager.Delete(t.Context(), &pb.DeleteProjectRequest{Id: p.Id}); err != nil {
				t.Fatal("unsafe legacy association cannot be unregistered", err)
			}
			if data, err := os.ReadFile(filepath.Join(chrome, "Cookies")); err != nil || string(data) != "private browser state" {
				t.Fatal("unregistering a protected association changed profile state", err)
			}
		})
	}
}

func TestProtectedProjectAdmissionRechecksRootAliases(t *testing.T) {
	f := setup(t)
	chrome := protectedBrowserState(t, f)
	p := create(t, f, "redirected")
	if err := os.Rename(p.Path, p.Path+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(chrome, p.Path); err != nil {
		t.Fatal(err)
	}
	if value, err := f.manager.Get(t.Context(), &pb.GetProjectRequest{Id: p.Id}); connect.CodeOf(err) != connect.CodePermissionDenied || value != nil {
		t.Fatal("stored association redirected into protected state", value, err)
	}
	if value, err := f.manager.ListDirectories(t.Context(), &pb.ListProjectDirectoriesRequest{ProjectId: p.Id}); connect.CodeOf(err) != connect.CodePermissionDenied || value != nil {
		t.Fatal("directory picker traversed redirected browser state", value, err)
	}
}

func TestProtectedProjectCreationReplayCannotRestoreLegacyAdmission(t *testing.T) {
	f := setup(t)
	chrome := protectedBrowserState(t, f)
	legacy, err := New(f.store, f.base, "test-user")
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	request := &pb.CreateProjectRequest{Name: "legacy-replay", Path: chrome, RequestId: "legacy-replay"}
	if _, err := legacy.Create(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if value, err := f.manager.Create(t.Context(), request); connect.CodeOf(err) != connect.CodePermissionDenied || value != nil {
		t.Fatal("saved creation receipt bypassed current host protection", value, err)
	}
}
