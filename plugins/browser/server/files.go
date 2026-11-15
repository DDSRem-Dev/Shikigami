package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
	"google.golang.org/protobuf/proto"
)

func validName(name string) bool {
	return len(name) > 0 && len(name) <= 240 && utf8.ValidString(name) && name != "." && name != ".." && !strings.HasPrefix(name, ".") && !strings.ContainsAny(name, "/\\") && strings.IndexFunc(name, unicode.IsControl) < 0 && strings.TrimSpace(name) == name
}
func validChecksum(checksum string) bool {
	decoded, err := hex.DecodeString(checksum)
	return err == nil && len(decoded) == sha256.Size && checksum == strings.ToLower(checksum)
}
func (worker *Worker) stage(ctx context.Context, profile, area string) (string, error) {
	if !browserruntime.ValidID(profile) || (area != "uploads" && area != "downloads") {
		return "", connect.NewError(connect.CodeInvalidArgument, "invalid profile staging area")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	worker.mu.Lock()
	exists := worker.db.Profiles[profile] != nil
	worker.mu.Unlock()
	if !exists {
		return "", connect.NewError(connect.CodeNotFound, "browser profile not found")
	}
	base := filepath.Join(worker.stateDir, "profiles")
	for _, path := range []string{base, filepath.Join(base, profile), filepath.Join(base, profile, area)} {
		if err := browserruntime.PrivateDir(path); err != nil {
			return "", err
		}
	}
	return filepath.Join(base, profile, area), nil
}
func (worker *Worker) lockFiles(ctx context.Context) (func(), error) {
	select {
	case worker.fileGate <- struct{}{}:
		return func() { <-worker.fileGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func hashFile(ctx context.Context, file *os.File, maximum int64) (int64, string, error) {
	hash := sha256.New()
	buffer := make([]byte, browserruntime.MaxWireFrame)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return 0, "", err
		}
		count, err := file.Read(buffer)
		if count > 0 {
			size += int64(count)
			if size > maximum {
				return 0, "", connect.NewError(connect.CodeResourceExhausted, "browser file exceeds limit")
			}
			_, _ = hash.Write(buffer[:count])
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, "", err
		}
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}
func (worker *Worker) ListBrowserFiles(parent context.Context, request *epb.BrowserFileRequest) (*epb.BrowserFileList, error) {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	if request == nil || request.Name != "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "listing requires an area, not an arbitrary path")
	}
	unlock, err := worker.lockFiles(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	directory, err := worker.stage(ctx, request.ProfileId, request.Area)
	if err != nil {
		return nil, err
	}
	entries, err := boundedEntries(directory)
	if err != nil {
		return nil, err
	}
	result := &epb.BrowserFileList{}
	var total int64
	for _, entry := range entries {
		if !validName(entry.Name()) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, "unsupported staging filename")
		}
		file, err := browserruntime.OpenStage(directory, entry.Name())
		if err != nil {
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		size, checksum, err := hashFile(ctx, file, worker.config.MaxFile)
		file.Close()
		if err != nil {
			return nil, err
		}
		total += size
		if total > worker.config.MaxStaging {
			return nil, connect.NewError(connect.CodeResourceExhausted, "browser staging quota exceeded")
		}
		result.Files = append(result.Files, &epb.BrowserFile{ProfileId: request.ProfileId, Area: request.Area, Name: entry.Name(), Size: size, Sha256: checksum, ModifiedAt: info.ModTime().Unix()})
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Name < result.Files[j].Name })
	return result, nil
}
func (worker *Worker) ReadBrowserFile(parent context.Context, request *epb.BrowserFileRequest, send func(*epb.BrowserFileChunk) error) error {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return err
	}
	defer done()
	ctx, cancelTransfer := context.WithTimeout(ctx, time.Duration(worker.config.FileSeconds)*time.Second)
	defer cancelTransfer()
	send, stopSender := cancellableChunkSender(ctx, send)
	defer stopSender()
	if request == nil || !validName(request.Name) {
		return connect.NewError(connect.CodeInvalidArgument, "safe staging filename required")
	}
	unlock, err := worker.lockFiles(ctx)
	if err != nil {
		return err
	}
	directory, err := worker.stage(ctx, request.ProfileId, request.Area)
	if err != nil {
		unlock()
		return err
	}
	file, err := browserruntime.OpenStage(directory, request.Name)
	unlock()
	if err != nil {
		return err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return err
	}
	size, checksum, err := hashFile(ctx, file, worker.config.MaxFile)
	if err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := send(&epb.BrowserFileChunk{ProfileId: request.ProfileId, Name: request.Name, TotalSize: size, Sha256: checksum}); err != nil {
		return err
	}
	hash := sha256.New()
	var transferred int64
	buffer := make([]byte, browserruntime.MaxWireFrame)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, err := file.Read(buffer)
		if count > 0 {
			transferred += int64(count)
			if transferred > size {
				return connect.NewError(connect.CodeDataLoss, "browser file grew while downloading")
			}
			_, _ = hash.Write(buffer[:count])
			if err := send(&epb.BrowserFileChunk{ProfileId: request.ProfileId, Name: request.Name, Data: buffer[:count]}); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if transferred != size || hex.EncodeToString(hash.Sum(nil)) != checksum || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return connect.NewError(connect.CodeDataLoss, "browser file changed while downloading")
	}
	return send(&epb.BrowserFileChunk{ProfileId: request.ProfileId, Name: request.Name, TotalSize: size, Sha256: checksum, Finish: true})
}

func (worker *Worker) WriteBrowserFile(parent context.Context, receive func(context.Context) (*epb.BrowserFileChunk, error)) (*epb.BrowserFile, error) {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	ctx, cancelTransfer := context.WithTimeout(ctx, time.Duration(worker.config.FileSeconds)*time.Second)
	defer cancelTransfer()
	// A canceled receiver is never read again, including after a terminal frame.
	firstCtx, firstCancel := context.WithTimeout(ctx, 10*time.Second)
	first, err := receive(firstCtx)
	firstCancel()
	if err != nil {
		return nil, err
	}
	if first == nil || proto.Size(first) > browserruntime.MaxWireFrame+4096 || !validName(first.Name) || !validRequestID(first.RequestId) || first.TotalSize < 0 || first.TotalSize > worker.config.MaxFile || len(first.Data) > browserruntime.MaxWireFrame || (first.Sha256 != "" && !validChecksum(first.Sha256)) {
		return nil, connect.NewError(connect.CodeInvalidArgument, "bounded upload metadata with request_id required")
	}
	unlock, err := worker.lockFiles(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	directory, err := worker.stage(ctx, first.ProfileId, "uploads")
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(directory, ".upload-*")
	if err != nil {
		return nil, err
	}
	temporary := filepath.Base(file.Name())
	defer file.Close()
	defer os.Remove(filepath.Join(directory, temporary))
	if err := file.Chmod(0600); err != nil {
		return nil, err
	}
	hash := sha256.New()
	var size int64
	checksum := first.Sha256
	frame := first
	frameNumber := 0
	for {
		if frame == nil || proto.Size(frame) > browserruntime.MaxWireFrame+4096 || len(frame.Data) > browserruntime.MaxWireFrame || (frameNumber > 0 && len(frame.Data) == 0 && !frame.Finish) || (frame.ProfileId != "" && frame.ProfileId != first.ProfileId) || (frame.Name != "" && frame.Name != first.Name) || (frame.RequestId != "" && frame.RequestId != first.RequestId) || (frame.ServerId != "" && frame.ServerId != first.ServerId) || (frame.TotalSize != 0 && frame.TotalSize != first.TotalSize) {
			return nil, connect.NewError(connect.CodeInvalidArgument, "upload metadata changed, frame exceeded its bound, or upload made no progress")
		}
		if frame.Sha256 != "" {
			if !validChecksum(frame.Sha256) || (checksum != "" && checksum != frame.Sha256) {
				return nil, connect.NewError(connect.CodeInvalidArgument, "upload checksum changed")
			}
			checksum = frame.Sha256
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		size += int64(len(frame.Data))
		if size > first.TotalSize || size > worker.config.MaxFile {
			return nil, connect.NewError(connect.CodeResourceExhausted, "upload exceeds declared size")
		}
		if _, err := file.Write(frame.Data); err != nil {
			return nil, err
		}
		_, _ = hash.Write(frame.Data)
		if frame.Finish {
			break
		}
		frameNumber++
		frame, err = receive(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, connect.NewError(connect.CodeDataLoss, "upload ended without finish")
			}
			return nil, err
		}
	}
	if size != first.TotalSize || !validChecksum(checksum) || checksum != hex.EncodeToString(hash.Sum(nil)) {
		return nil, connect.NewError(connect.CodeDataLoss, "upload size/checksum mismatch")
	}
	if extra, err := receive(ctx); !errors.Is(err, io.EOF) || extra != nil {
		if err != nil {
			return nil, err
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, "upload has data after finish")
	}
	identity := struct {
		Profile, Name, Checksum string
		Size                    int64
	}{first.ProfileId, first.Name, checksum, size}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	var replay epb.BrowserFile
	if found, err := worker.replayLocked(first.RequestId, "upload", identity, &replay); found || err != nil {
		return &replay, err
	}
	if worker.db.Profiles[first.ProfileId] == nil {
		return nil, connect.NewError(connect.CodeNotFound, "profile was deleted")
	}
	if existing, err := browserruntime.OpenStage(directory, first.Name); err == nil {
		existingSize, existingChecksum, hashErr := hashFile(ctx, existing, worker.config.MaxFile)
		info, statErr := existing.Stat()
		existing.Close()
		if hashErr != nil {
			return nil, hashErr
		}
		if statErr != nil {
			return nil, statErr
		}
		if existingSize != size || existingChecksum != checksum {
			return nil, connect.NewError(connect.CodeAlreadyExists, "upload filename exists with different content; choose a new filename")
		}
		result := &epb.BrowserFile{ProfileId: first.ProfileId, Area: "uploads", Name: first.Name, Size: size, Sha256: checksum, ModifiedAt: info.ModTime().Unix()}
		if err := worker.rememberLocked(first.RequestId, "upload", identity, result); err != nil {
			return nil, err
		}
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := worker.checkStagingQuota(ctx, first.ProfileId, size, temporary); err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	temporaryInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Link is a no-replace publication, then remove the private temporary name.
	// Startup unlinks interrupted temporary names before validating final files.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := root.Link(temporary, first.Name); err != nil {
		return nil, err
	}
	if err := root.Remove(temporary); err != nil {
		root.Remove(first.Name)
		return nil, err
	}
	published, err := browserruntime.OpenStage(directory, first.Name)
	if err != nil {
		root.Remove(first.Name)
		return nil, err
	}
	publishedInfo, statErr := published.Stat()
	published.Close()
	if statErr != nil || !os.SameFile(temporaryInfo, publishedInfo) {
		root.Remove(first.Name)
		return nil, connect.NewError(connect.CodeDataLoss, "upload publication identity changed")
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	err = directoryHandle.Sync()
	directoryHandle.Close()
	if err != nil {
		return nil, err
	}
	result := &epb.BrowserFile{ProfileId: first.ProfileId, Area: "uploads", Name: first.Name, Size: size, Sha256: checksum, ModifiedAt: time.Now().Unix()}
	if err := worker.rememberLocked(first.RequestId, "upload", identity, result); err != nil {
		return nil, err
	}
	return result, nil
}
func (worker *Worker) checkStagingQuota(ctx context.Context, profile string, additional int64, temporary string) error {
	total := additional
	count := 0
	for _, area := range []string{"uploads", "downloads"} {
		directory := filepath.Join(worker.stateDir, "profiles", profile, area)
		if err := browserruntime.PrivateDir(directory); err != nil {
			return err
		}
		entries, err := boundedEntries(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if area == "uploads" && entry.Name() == temporary {
				continue
			}
			count++
			if !validName(entry.Name()) {
				return connect.NewError(connect.CodeFailedPrecondition, "unsafe staging entry")
			}
			file, err := browserruntime.OpenStage(directory, entry.Name())
			if err != nil {
				return err
			}
			info, err := file.Stat()
			file.Close()
			if err != nil {
				return err
			}
			total += info.Size()
			if count >= stagingEntryLimit || total > worker.config.MaxStaging {
				return connect.NewError(connect.CodeResourceExhausted, "browser staging quota exceeded")
			}
		}
	}
	return nil
}
