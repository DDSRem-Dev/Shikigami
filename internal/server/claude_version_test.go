package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestClaudeVersionUsesBoundedRealExecutable(t *testing.T) {
	for _, fixture := range []struct {
		name, body string
		success    bool
	}{
		{"version", "printf '2.1.3-test (Claude Code)\\n'", true},
		{"invalid", "printf 'unrecognized version\\n'", false},
		{"oversized", "printf '2.1.3-test ' ; i=0; while [ \"$i\" -lt 5000 ]; do printf x; i=$((i+1)); done", false},
		{"timeout", "exec sleep 10", false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "claude-fixture")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+fixture.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			s := &Service{ClaudeExecutable: path}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			start := time.Now()
			version, err := s.GetClaudeVersion(ctx, &pb.Empty{})
			if fixture.success {
				if err != nil || version.Version != "2.1.3-test" || version.Path != path {
					t.Fatalf("version query: %v %v", version, err)
				}
			} else if err == nil {
				t.Fatal("invalid/unbounded version accepted")
			}
			if time.Since(start) > 4500*time.Millisecond {
				t.Fatal("version timeout was not bounded")
			}
		})
	}
}
