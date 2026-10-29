package server

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

var claudeVersionPattern = regexp.MustCompile(`(?:^|\s)([0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9._-]+)?)`)

type versionOutput struct{ buffer bytes.Buffer }

func (w *versionOutput) String() string { return w.buffer.String() }

func (w *versionOutput) Write(data []byte) (int, error) {
	if w.buffer.Len()+len(data) > 4096 {
		return 0, errors.New("version output exceeds limit")
	}
	return w.buffer.Write(data)
}

func (s *Service) GetClaudeVersion(parent context.Context, _ *pb.Empty) (*pb.ClaudeVersion, error) {
	name := s.ClaudeExecutable
	if name == "" {
		name = "claude"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "Claude executable is unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = time.Second
	var output versionOutput
	cmd.Stdout = &output
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, "Claude version query failed")
	}
	match := claudeVersionPattern.FindStringSubmatch(strings.TrimSpace(output.String()))
	if len(match) != 2 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "Claude returned an unrecognized version")
	}
	return &pb.ClaudeVersion{Version: match[1], Path: path}, nil
}
