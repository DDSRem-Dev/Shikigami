package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

const maxConflictPreview = 8 << 20

type conflictReader interface {
	Receive() (*pb.FileData, error)
	Close() error
}

func (a *app) showConflictDiff(ctx context.Context, c *client.Client, conflict *ppb.Conflict) error {
	dir, e := os.MkdirTemp("", "shiki-diff-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	// MkdirTemp creates 0700, and every file is explicitly created as 0600.
	sides := []struct {
		name    string
		side    ppb.ReadConflictFileRequest_Side
		version *ppb.FileVersion
	}{{"local", ppb.ReadConflictFileRequest_LOCAL, conflict.Local}, {"remote", ppb.ReadConflictFileRequest_REMOTE, conflict.Remote}}
	labels := make([]string, 2)
	paths := make([]string, 2)
	for index, side := range sides {
		if side.version == nil {
			return errors.New("conflict contains no observed side version; refresh conflicts")
		}
		labels[index] = side.name + "/" + safeText(conflict.Path)
		if !side.version.Exists {
			labels[index] += " (deleted)"
		}
		paths[index] = filepath.Join(dir, side.name)
		file, e := os.OpenFile(paths[index], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		if side.version.Exists {
			stream, readErr := c.Agent().ReadConflictFile(ctx, &ppb.ReadConflictFileRequest{MappingId: conflict.MappingId, ConflictId: conflict.Id, ExpectedGeneration: conflict.Generation, Side: side.side})
			if readErr != nil {
				file.Close()
				return readErr
			}
			readErr = writeConflictPreview(file, stream, side.version)
			closeErr := stream.Close()
			fileErr := file.Close()
			if e = errors.Join(readErr, closeErr, fileErr); e != nil {
				return e
			}
		} else if e = file.Close(); e != nil {
			return e
		}
	}
	output, e := exec.CommandContext(ctx, "diff", "-u", "-L", labels[0], "-L", labels[1], paths[0], paths[1]).CombinedOutput()
	var exit *exec.ExitError
	if e != nil && (!errors.As(e, &exit) || exit.ExitCode() != 1) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cannot compare conflict versions: %w", e)
	}
	result := struct {
		Path         string `json:"path"`
		Generation   string `json:"generation"`
		Initial      bool   `json:"initial_sync"`
		LocalExists  bool   `json:"local_exists"`
		RemoteExists bool   `json:"remote_exists"`
		Diff         string `json:"diff"`
	}{conflict.Path, conflict.Generation, conflict.Base == nil, conflict.Local.Exists, conflict.Remote.Exists, string(output)}
	if a.json {
		return a.printJSON(result)
	}
	if _, e = fmt.Fprintf(a.output, "%s ↔ %s\nGeneration: %s\n", labels[0], labels[1], safeText(conflict.Generation)); e != nil {
		return e
	}
	if conflict.Base == nil {
		if _, e = fmt.Fprintln(a.output, "Initial sync: no shared base; choose keep-local, keep-remote, or keep-both explicitly."); e != nil {
			return e
		}
	}
	if len(output) == 0 {
		_, e = fmt.Fprintln(a.output, "Contents are equal; existence and file modes may still differ.")
		return e
	}
	_, e = fmt.Fprint(a.output, safeDiffText(string(output)))
	return e
}
func writeConflictPreview(file io.Writer, stream conflictReader, version *ppb.FileVersion) error {
	first, e := stream.Receive()
	if e != nil {
		return e
	}
	info := first.GetInfo()
	if info == nil || info.IsDir || info.Size < 0 || info.Size > maxConflictPreview {
		return errors.New("conflict preview requires a regular file no larger than 8 MiB")
	}
	if info.ContentHash == "" || info.ContentHash != version.ContentHash {
		return errors.New("conflict preview version changed; refresh conflicts")
	}
	hash := sha256.New()
	writer := io.MultiWriter(file, hash)
	var total int64
	for {
		frame, e := stream.Receive()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return e
		}
		payload, ok := frame.Payload.(*pb.FileData_Chunk)
		if !ok {
			return errors.New("unexpected metadata in conflict file stream")
		}
		total += int64(len(payload.Chunk))
		if total > info.Size || total > maxConflictPreview {
			return errors.New("conflict preview exceeds its declared size")
		}
		if _, e = writer.Write(payload.Chunk); e != nil {
			return e
		}
	}
	if total != info.Size || hex.EncodeToString(hash.Sum(nil)) != info.ContentHash {
		return errors.New("conflict preview content did not match the observed version")
	}
	return nil
}
func safeDiffText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 32 || r == 127 {
			return '�'
		}
		return r
	}, value)
}
