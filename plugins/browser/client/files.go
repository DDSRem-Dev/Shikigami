package client

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

const fileChunkBytes = 64 << 10

func validFileName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 240 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	var digest [sha256.Size]byte
	_, err := hex.Decode(digest[:], []byte(value))
	return err == nil
}

func validArea(area string) bool { return area == "uploads" || area == "downloads" }

func exactQuery(query url.Values, keys ...string) bool {
	if len(query) != len(keys) {
		return false
	}
	for _, key := range keys {
		if len(query[key]) != 1 {
			return false
		}
	}
	return true
}

func (w *Worker) handleFileList(out http.ResponseWriter, r *http.Request, v *viewer) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !exactQuery(query, "area") || !validArea(query.Get("area")) {
		writeHTTPError(out, connect.NewError(connect.CodeInvalidArgument, "INVALID_STAGING_AREA"))
		return
	}
	ctx, cancel := viewerRequestContext(r, v, w.config.operationTimeout)
	defer cancel()
	files, err := w.remote.ListBrowserFiles(ctx, &epb.BrowserFileRequest{ServerId: v.serverID, ProfileId: v.profileID, Area: query.Get("area")})
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	if files == nil {
		writeHTTPError(out, errors.New("empty file list"))
		return
	}
	writeJSON(out, http.StatusOK, files)
}

func acquireTransfer(out http.ResponseWriter, v *viewer) bool {
	select {
	case v.fileSlot <- struct{}{}:
		return true
	default:
		writeHTTPError(out, connect.NewError(connect.CodeResourceExhausted, "VIEWER_TRANSFER_ALREADY_ACTIVE"))
		return false
	}
}

func (w *Worker) transferBudget(size int64) time.Duration {
	// This is a bounded operation budget, not an empirically measured throughput
	// claim. The viewer's fixed session expiry is an additional upper bound.
	return w.config.operationTimeout + time.Duration((size+fileChunkBytes-1)/fileChunkBytes)*time.Second
}

func transferDeadline(out http.ResponseWriter, r *http.Request, ctx context.Context) func() {
	controller := http.NewResponseController(out)
	body := r.Body
	finished := make(chan struct{})
	if deadline, ok := ctx.Deadline(); ok {
		controller.SetReadDeadline(deadline)
		controller.SetWriteDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() {
		defer close(finished)
		// A body Read can hold net/http's body mutex. Interrupt its socket
		// first; Close alone may otherwise wait behind that blocked Read.
		controller.SetReadDeadline(time.Now())
		controller.SetWriteDeadline(time.Now())
		body.Close()
	})
	return func() {
		if !stop() {
			<-finished
		}
		controller.SetReadDeadline(time.Time{})
		controller.SetWriteDeadline(time.Time{})
	}
}

func (w *Worker) handleUpload(out http.ResponseWriter, r *http.Request, v *viewer) {
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	size, sizeErr := strconv.ParseInt(query.Get("size"), 10, 64)
	name, checksum := query.Get("name"), query.Get("sha256")
	if queryErr != nil || !exactQuery(query, "name", "size", "sha256") || sizeErr != nil || size < 0 || size > w.config.maxFileBytes || !validFileName(name) || !validSHA256(checksum) || r.ContentLength >= 0 && r.ContentLength != size {
		writeHTTPError(out, connect.NewError(connect.CodeInvalidArgument, "INVALID_BROWSER_UPLOAD"))
		return
	}
	if !acquireTransfer(out, v) {
		return
	}
	defer func() { <-v.fileSlot }()
	ctx, cancel := viewerRequestContext(r, v, w.transferBudget(size))
	defer cancel()
	resetDeadline := transferDeadline(out, r, ctx)
	defer resetDeadline()
	r.Body = http.MaxBytesReader(out, r.Body, size+1)
	requestID, err := randomSecret(24)
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	stream, err := w.remote.WriteBrowserFile(ctx)
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	defer stream.Close()
	header := &epb.BrowserFileChunk{ServerId: v.serverID, ProfileId: v.profileID, Name: name, TotalSize: size, Sha256: checksum, RequestId: requestID}
	if err := stream.Send(header); err != nil {
		writeHTTPError(out, err)
		return
	}
	hash := sha256.New()
	buffer := make([]byte, fileChunkBytes)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			writeHTTPError(out, err)
			return
		}
		n, readErr := r.Body.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > size {
				writeHTTPError(out, connect.NewError(connect.CodeInvalidArgument, "UPLOAD_SIZE_MISMATCH"))
				return
			}
			hash.Write(buffer[:n])
			// Send is synchronous; its transport has consumed the bytes before the
			// buffer is reused. There is exactly one sender on this stream.
			if err := stream.Send(&epb.BrowserFileChunk{ServerId: v.serverID, ProfileId: v.profileID, Name: name, RequestId: requestID, Data: buffer[:n]}); err != nil {
				writeHTTPError(out, err)
				return
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			writeHTTPError(out, connect.NewError(connect.CodeInvalidArgument, "UPLOAD_BODY_INCOMPLETE"))
			return
		}
	}
	var expected [sha256.Size]byte
	hex.Decode(expected[:], []byte(checksum))
	var digest [sha256.Size]byte
	actual := hash.Sum(digest[:0])
	if total != size || subtle.ConstantTimeCompare(actual, expected[:]) != 1 {
		writeHTTPError(out, connect.NewError(connect.CodeInvalidArgument, "UPLOAD_INTEGRITY_MISMATCH"))
		return
	}
	if err := stream.Send(&epb.BrowserFileChunk{ServerId: v.serverID, ProfileId: v.profileID, Name: name, RequestId: requestID, Sha256: checksum, Finish: true}); err != nil {
		writeUploadOutcomeError(out, err)
		return
	}
	result, err := stream.CloseAndReceive()
	if err != nil {
		writeUploadOutcomeError(out, err)
		return
	}
	if result == nil || result.ProfileId != v.profileID || result.Name != name || result.Area != "uploads" || result.Size != size || result.Sha256 != checksum {
		writeUploadOutcomeError(out, connect.NewError(connect.CodeDataLoss, "UPLOAD_ACK_MISMATCH"))
		return
	}
	writeJSON(out, http.StatusOK, result)
}

func writeUploadOutcomeError(out http.ResponseWriter, err error) {
	status, body := safeError(err)
	body.Message += " Upload commit outcome is unknown. Refresh uploads and compare its SHA-256 before any retry; this viewer never retries an upload automatically."
	writeJSON(out, status, body)
}

func (w *Worker) handleDownload(out http.ResponseWriter, r *http.Request, v *viewer) {
	var request struct {
		Area   string `json:"area"`
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	}
	if !decodeJSON(out, r, &request) {
		return
	}
	if !validArea(request.Area) || !validFileName(request.Name) || request.Size < 0 || request.Size > w.config.maxFileBytes || !validSHA256(request.SHA256) {
		writeHTTPError(out, connect.NewError(connect.CodeInvalidArgument, "INVALID_BROWSER_DOWNLOAD"))
		return
	}
	if !acquireTransfer(out, v) {
		return
	}
	defer func() { <-v.fileSlot }()
	ctx, cancel := viewerRequestContext(r, v, w.transferBudget(request.Size))
	defer cancel()
	resetDeadline := transferDeadline(out, r, ctx)
	defer resetDeadline()
	// The user selected a particular file version. Recheck its current metadata
	// and later the complete stream, rather than silently retrieving new bytes.
	listing, err := w.remote.ListBrowserFiles(ctx, &epb.BrowserFileRequest{ServerId: v.serverID, ProfileId: v.profileID, Area: request.Area})
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	matched := false
	if listing != nil {
		for _, file := range listing.Files {
			if file.ProfileId == v.profileID && file.Area == request.Area && file.Name == request.Name && file.Size == request.Size && file.Sha256 == request.SHA256 {
				matched = true
				break
			}
		}
	}
	if !matched {
		writeHTTPError(out, connect.NewError(connect.CodeAborted, "STAGING_FILE_CHANGED"))
		return
	}
	chunks, err := w.remote.ReadBrowserFile(ctx, &epb.BrowserFileRequest{ServerId: v.serverID, ProfileId: v.profileID, Area: request.Area, Name: request.Name})
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	defer chunks.Close()
	file, err := os.CreateTemp(w.transferDir, "download-")
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	defer func() { file.Close(); os.Remove(file.Name()) }()
	hash := sha256.New()
	var total int64
	first, finished := true, false
	for {
		if err := ctx.Err(); err != nil {
			writeHTTPError(out, err)
			return
		}
		chunk, receiveErr := chunks.Receive()
		if errors.Is(receiveErr, io.EOF) {
			if first || !finished {
				writeHTTPError(out, connect.NewError(connect.CodeDataLoss, "DOWNLOAD_MISSING_TERMINAL"))
				return
			}
			break
		}
		if receiveErr != nil {
			writeHTTPError(out, receiveErr)
			return
		}
		if chunk == nil || finished || len(chunk.Data) > fileChunkBytes || chunk.ServerId != "" && chunk.ServerId != v.serverID || chunk.ProfileId != "" && chunk.ProfileId != v.profileID || chunk.Name != "" && chunk.Name != request.Name || chunk.TotalSize != 0 && chunk.TotalSize != request.Size || chunk.Sha256 != "" && chunk.Sha256 != request.SHA256 {
			writeHTTPError(out, connect.NewError(connect.CodeDataLoss, "DOWNLOAD_FRAME_MISMATCH"))
			return
		}
		if first {
			if chunk.ProfileId != v.profileID || chunk.Name != request.Name || chunk.TotalSize != request.Size || chunk.Sha256 != request.SHA256 {
				writeHTTPError(out, connect.NewError(connect.CodeDataLoss, "DOWNLOAD_HEADER_MISMATCH"))
				return
			}
			first = false
		}
		total += int64(len(chunk.Data))
		if total > request.Size {
			writeHTTPError(out, connect.NewError(connect.CodeDataLoss, "DOWNLOAD_SIZE_EXCEEDED"))
			return
		}
		if len(chunk.Data) > 0 {
			if _, err := file.Write(chunk.Data); err != nil {
				writeHTTPError(out, err)
				return
			}
			hash.Write(chunk.Data)
		}
		if chunk.Finish {
			if chunk.Sha256 != request.SHA256 {
				writeHTTPError(out, connect.NewError(connect.CodeDataLoss, "DOWNLOAD_TERMINAL_MISMATCH"))
				return
			}
			finished = true
		}
	}
	var expected [sha256.Size]byte
	hex.Decode(expected[:], []byte(request.SHA256))
	var digest [sha256.Size]byte
	actual := hash.Sum(digest[:0])
	if total != request.Size || subtle.ConstantTimeCompare(actual, expected[:]) != 1 {
		writeHTTPError(out, connect.NewError(connect.CodeDataLoss, "DOWNLOAD_INTEGRITY_MISMATCH"))
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeHTTPError(out, err)
		return
	}
	if err := ctx.Err(); err != nil {
		writeHTTPError(out, err)
		return
	}
	// HTTP success begins only after the typed RPC terminal status and checksum
	// have both succeeded. Partial remote files never become browser downloads.
	out.Header().Set("Content-Type", "application/octet-stream")
	out.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": request.Name}))
	out.Header().Set("X-Shiki-SHA256", request.SHA256)
	http.ServeContent(out, r.WithContext(ctx), request.Name, time.Time{}, file)
}
