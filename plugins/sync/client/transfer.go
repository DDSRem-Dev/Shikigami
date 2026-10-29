package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	limits "github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

func observation(ctx context.Context, files sdk.FileService, project, path string, maxBytes int64) (*ppb.FileVersion, *pb.FileInfo, error) {
	info, err := files.StatFile(ctx, &pb.StatFileRequest{ProjectId: project, Path: path})
	if connect.CodeOf(err) == connect.CodeNotFound {
		return &ppb.FileVersion{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if info == nil || info.IsDir {
		return nil, nil, fail(connect.CodeFailedPrecondition, "PATH_NOT_REGULAR_FILE")
	}
	if info.Size > maxBytes {
		return nil, info, fail(connect.CodeResourceExhausted, "FILE_TOO_LARGE")
	}
	return &ppb.FileVersion{Exists: true, ContentHash: info.ContentHash, Mode: info.Mode, Revision: info.Revision}, info, nil
}

// transfer stages a bounded, verified snapshot privately on disk before a
// conditional destination write. No source/destination lock is held together.
func (t *task) transfer(ctx context.Context, source, destination sdk.FileService, sourcePath, targetPath string, observed, target *ppb.FileVersion) error {
	if !observed.Exists {
		if !target.Exists {
			return nil
		}
		if err := t.prepare(source, destination, sourcePath, targetPath, observed, target, "", 0); err != nil {
			return err
		}
		if err := destination.DeleteFile(ctx, &pb.DeleteFileRequest{ProjectId: t.data.Mapping.ProjectId, Path: targetPath, ExpectedRevision: target.Revision}); err != nil {
			return err
		}
		return t.committed(targetPath, &ppb.FileVersion{})
	}
	file, err := t.stageDir.CreateTemp()
	if err != nil {
		return err
	}
	defer func() {
		file.Close()
		if !t.referencesStage(filepath.Base(file.Name())) {
			t.stageDir.Remove(filepath.Base(file.Name()))
		}
	}()
	var info *pb.FileInfo
	hash := sha256.New()
	var size int64
	err = source.ReadFile(ctx, &pb.ReadFileRequest{ProjectId: t.data.Mapping.ProjectId, Path: sourcePath, ExpectedRevision: observed.Revision}, func(frame *pb.FileData) error {
		if frame == nil {
			return fail(connect.CodeDataLoss, "EMPTY_FILE_FRAME")
		}
		if info == nil {
			info = frame.GetInfo()
			if info == nil || info.IsDir || info.Revision != observed.Revision || info.ContentHash != observed.ContentHash || info.Mode != observed.Mode || info.Size < 0 || info.Size > t.worker.maxFileBytes {
				return fail(connect.CodeAborted, "SOURCE_CHANGED")
			}
			return nil
		}
		chunk, ok := frame.Payload.(*pb.FileData_Chunk)
		if !ok {
			return fail(connect.CodeDataLoss, "FILE_INFO_REPEATED")
		}
		if len(chunk.Chunk) > limits.MaxDataBytes {
			return fail(connect.CodeResourceExhausted, "FILE_CHUNK_TOO_LARGE")
		}
		size += int64(len(chunk.Chunk))
		if size > info.Size || size > t.worker.maxFileBytes {
			return fail(connect.CodeDataLoss, "FILE_SIZE_MISMATCH")
		}
		if _, err := hash.Write(chunk.Chunk); err != nil {
			return err
		}
		_, err := file.Write(chunk.Chunk)
		return err
	})
	if err != nil {
		return err
	}
	if info == nil || size != info.Size || hex.EncodeToString(hash.Sum(nil)) != observed.ContentHash {
		return fail(connect.CodeDataLoss, "FILE_HASH_MISMATCH")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	header := &pb.WriteFileHeader{ProjectId: t.data.Mapping.ProjectId, Path: targetPath, ContentHash: observed.ContentHash, Size: info.Size, Mode: observed.Mode}
	if target.Exists {
		header.BaseRevision = target.Revision
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = t.stageDir.Sync(); err != nil {
		return err
	}
	if err = t.prepare(source, destination, sourcePath, targetPath, observed, target, filepath.Base(file.Name()), size); err != nil {
		return err
	}
	first := true
	var emitted int64
	buffer := make([]byte, limits.MaxDataBytes)
	result, err := destination.WriteFile(ctx, func() (*pb.WriteFileRequest, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if first {
			if size == 0 {
				if err := t.allowCommit(targetPath); err != nil {
					return nil, err
				}
			}
			first = false
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: header}}, nil
		}
		n, err := file.Read(buffer)
		if n > 0 {
			emitted += int64(n)
			if emitted > size {
				return nil, fail(connect.CodeDataLoss, "STAGED_SOURCE_SIZE_CHANGED")
			}
			if emitted == size {
				if err := t.allowCommit(targetPath); err != nil {
					return nil, err
				}
			}
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: append([]byte(nil), buffer[:n]...)}}, nil
		}
		return nil, err
	})
	if err != nil {
		return err
	}
	if result == nil || result.Path != targetPath || result.Revision == "" || result.ContentHash != observed.ContentHash || result.Mode != observed.Mode || result.Size != size {
		return fail(connect.CodeDataLoss, "DESTINATION_COMMIT_MISMATCH")
	}
	return t.committed(targetPath, fileVersion(result))
}
func classify(err error) (string, string) {
	switch connect.CodeOf(err) {
	case connect.CodeUnauthenticated:
		return "auth_required", "REMOTE_AUTH_REQUIRED"
	case connect.CodeUnavailable, connect.CodeDeadlineExceeded:
		return "offline", "REMOTE_UNAVAILABLE"
	case connect.CodeAborted:
		return "reconciling", "FILE_CHANGED_RESCAN_REQUIRED"
	case connect.CodeResourceExhausted:
		return "error", "FILE_LIMIT_EXCEEDED"
	}
	if errors.Is(err, context.Canceled) {
		return "paused", "TASK_CANCELED"
	}
	return "error", "SYNC_IO_FAILED"
}
