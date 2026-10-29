package config

import (
	"reflect"
	"testing"
)

func TestTemplateSchemaAndRemoteDirectories(t *testing.T) {
	no := false
	cfg := DefaultClient()
	cfg.Templates = map[string]SessionTemplate{"fullstack-dev": {Description: "Remote sessions", Sessions: []TemplateSession{{Name: "frontend", WorkingDir: "frontend", Metadata: map[string]string{"language": "typescript"}}, {Name: "database", WorkingDir: ".", AutoStart: &no}}}}
	if e := cfg.NormalizeAndValidate(); e != nil {
		t.Fatal(e)
	}
	if cfg.Templates["fullstack-dev"].Sessions[0].WorkingDir != "frontend" || cfg.Templates["fullstack-dev"].Sessions[0].AutoStart != nil {
		t.Fatal("template resolved a remote directory locally or lost default auto-start semantics")
	}
	for _, directory := range []string{"/absolute", "../escape", "a/../escape", "./work", "a\\b", "a\x00b"} {
		if ValidTemplateDirectory(directory) {
			t.Fatalf("invalid remote-relative directory accepted: %q", directory)
		}
	}
	for _, field := range []string{"Token", "env", "api_key", "command", "claude_context_path"} {
		template := cfg.Templates["fullstack-dev"]
		template.Sessions[0].Metadata = map[string]string{field: "secret"}
		cfg.Templates["fullstack-dev"] = template
		if e := cfg.NormalizeAndValidate(); e == nil {
			t.Fatalf("secret or executable template metadata accepted: %s", field)
		}
	}
}
func TestSnapshotMetadataExplicitAllowlist(t *testing.T) {
	input := map[string]string{"color": "#102030", "icon": "terminal", "language": "go", "role": "backend", "Token": "shiki_secret", "HOME": "/private", "claude_context_path": "/conversations"}
	want := map[string]string{"color": "#102030", "icon": "terminal", "language": "go", "role": "backend"}
	if got := SnapshotMetadata(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata filter: %v", got)
	}
	for _, value := range []string{"shiki_secret", "tskey-auth-secret", "sk-secret", "a\nb", "Bearer token"} {
		if len(SnapshotMetadata(map[string]string{"role": value})) != 0 {
			t.Fatalf("credential-shaped presentation value accepted: %q", value)
		}
	}
}
