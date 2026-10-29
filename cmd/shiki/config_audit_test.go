package main

import (
	"bytes"
	"context"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestConfigurationAuditBoundsAndReadOnlyServerFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(2 << 20); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := readConfigurationEdit(path); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatal("oversize edit accepted", err)
	}
	before := &apb.ServerRecord{Config: &apb.ServerConfig{Id: "srv_test", Name: "name"}, Revision: "rev", CredentialPresent: true, CredentialRevision: "cred", ConnectionState: "ready"}
	for _, mutate := range []func(*apb.ServerRecord){
		func(v *apb.ServerRecord) { v.Config.Id = "srv_other" }, func(v *apb.ServerRecord) { v.Revision = "new" }, func(v *apb.ServerRecord) { v.CredentialPresent = false }, func(v *apb.ServerRecord) { v.CredentialRevision = "other" }, func(v *apb.ServerRecord) { v.ConnectionState = "offline" },
	} {
		after := proto.Clone(before).(*apb.ServerRecord)
		mutate(after)
		if err := validateConfigurationEdit(before, after); err == nil {
			t.Fatal("read-only server field accepted", after)
		}
	}
}
func TestConfigurationEditorCancellationReleasesInheritedOutputPipes(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	editor := filepath.Join(dir, "editor")
	script := "#!/bin/sh\nsleep 30 &\nprintf '%s' \"$!\" > \"$SHIKI_EDITOR_TEST_PID\"\nwait\n"
	if err := os.WriteFile(editor, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", editor)
	t.Setenv("SHIKI_EDITOR_TEST_PID", pidPath)
	t.Cleanup(func() {
		data, _ := os.ReadFile(pidPath)
		pid, _ := strconv.Atoi(string(data))
		if pid > 0 {
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	a := &app{output: &bytes.Buffer{}, errorOutput: &bytes.Buffer{}}
	started := time.Now()
	_, err := a.editConfiguration(ctx, &apb.Preferences{Revision: "revision"})
	if err == nil || time.Since(started) > 3*time.Second {
		t.Fatal("canceled editor waited for descendant-held output", err)
	}
}
