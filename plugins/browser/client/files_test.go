package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/proto"
)

type viewerTestDownload struct {
	frames   []*epb.BrowserFileChunk
	index    int
	terminal error
	closed   chan struct{}
	once     sync.Once
}

func (d *viewerTestDownload) Receive() (*epb.BrowserFileChunk, error) {
	if d.index < len(d.frames) {
		frame := d.frames[d.index]
		d.index++
		return frame, nil
	}
	if d.terminal != nil {
		return nil, d.terminal
	}
	return nil, io.EOF
}
func (d *viewerTestDownload) Close() error { d.once.Do(func() { close(d.closed) }); return nil }

func fileChecksum(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func TestViewerDownloadRequiresIntegrityAndTerminalSuccess(t *testing.T) {
	contents := []byte("download contents\n")
	checksum := fileChecksum(contents)
	metadata := &epb.BrowserFileChunk{ProfileId: "profile", Name: "example.txt", TotalSize: int64(len(contents)), Sha256: checksum}
	data := &epb.BrowserFileChunk{ProfileId: "profile", Name: "example.txt", Data: contents}
	finish := &epb.BrowserFileChunk{ProfileId: "profile", Name: "example.txt", TotalSize: int64(len(contents)), Sha256: checksum, Finish: true}
	for _, scenario := range []struct {
		name     string
		frames   []*epb.BrowserFileChunk
		terminal error
		code     int
	}{
		{"complete", []*epb.BrowserFileChunk{metadata, data, finish}, nil, http.StatusOK},
		{"wrapped EOF", []*epb.BrowserFileChunk{metadata, data, finish}, connect.NewError(connect.CodeUnknown, "terminal EOF").WithCause(io.EOF), http.StatusOK},
		{"wrapped EOF without finish", []*epb.BrowserFileChunk{metadata, data}, connect.NewError(connect.CodeUnknown, "terminal EOF").WithCause(io.EOF), http.StatusBadGateway},
		{"missing finish", []*epb.BrowserFileChunk{metadata, data}, nil, http.StatusBadGateway},
		{"failed terminal", []*epb.BrowserFileChunk{metadata, data, finish}, connect.NewError(connect.CodeUnavailable, "lost terminal"), http.StatusServiceUnavailable},
		{"wrong bytes", []*epb.BrowserFileChunk{metadata, {Data: bytes.Repeat([]byte{'x'}, len(contents))}, finish}, nil, http.StatusBadGateway},
		{"extra after finish", []*epb.BrowserFileChunk{metadata, data, finish, {Data: []byte("extra")}}, nil, http.StatusBadGateway},
		{"wrong profile", []*epb.BrowserFileChunk{{ProfileId: "other", Name: "example.txt", TotalSize: int64(len(contents)), Sha256: checksum}, data, finish}, nil, http.StatusBadGateway},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			receiver := &viewerTestDownload{frames: scenario.frames, terminal: scenario.terminal, closed: make(chan struct{})}
			browser := &viewerTestBrowser{
				list: func(_ context.Context, request *epb.BrowserFileRequest) (*epb.BrowserFileList, error) {
					if request.ServerId != "server" || request.ProfileId != "profile" || request.Area != "downloads" || request.Name != "" {
						t.Errorf("file listing scope changed: %v", request)
					}
					return &epb.BrowserFileList{Files: []*epb.BrowserFile{{ProfileId: "profile", Area: "downloads", Name: "example.txt", Size: int64(len(contents)), Sha256: checksum}}}, nil
				},
				read: func(_ context.Context, request *epb.BrowserFileRequest) (sdk.Receiver[epb.BrowserFileChunk], error) {
					if request.ServerId != "server" || request.ProfileId != "profile" || request.Area != "downloads" || request.Name != "example.txt" {
						t.Errorf("file download scope changed: %v", request)
					}
					return receiver, nil
				},
			}
			w := startViewerTestWorker(t, browser)
			viewer, client, csrf := bootstrapViewerTest(t, w, true)
			body, _ := json.Marshal(map[string]any{"area": "downloads", "name": "example.txt", "size": len(contents), "sha256": checksum})
			response := viewerTestRequest(t, client, http.MethodPost, w.origin+"/v/"+viewer.Id+"/api/files/download", w.origin, csrf, body)
			result, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != scenario.code {
				t.Fatalf("HTTP %d, want %d: %s", response.StatusCode, scenario.code, result)
			}
			if scenario.code == http.StatusOK {
				if !bytes.Equal(result, contents) || response.Header.Get("X-Shiki-SHA256") != checksum || !strings.HasPrefix(response.Header.Get("Content-Disposition"), "attachment;") {
					t.Fatal("verified download metadata/body missing")
				}
			} else if response.Header.Get("Content-Disposition") != "" || bytes.Equal(result, contents) {
				t.Fatal("incomplete file was exposed as a successful download")
			}
			<-receiver.closed
			entries, err := os.ReadDir(w.transferDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("private download spool leaked: %v %v", entries, err)
			}
		})
	}
}

type viewerTestUpload struct {
	frames []*epb.BrowserFileChunk
	result *epb.BrowserFile
	closed chan struct{}
	once   sync.Once
}

func (u *viewerTestUpload) Send(frame *epb.BrowserFileChunk) error {
	u.frames = append(u.frames, proto.Clone(frame).(*epb.BrowserFileChunk))
	return nil
}
func (u *viewerTestUpload) CloseSend() error { return nil }
func (u *viewerTestUpload) Receive() (*epb.BrowserFile, error) {
	return u.CloseAndReceive()
}
func (u *viewerTestUpload) CloseAndReceive() (*epb.BrowserFile, error) {
	if u.result != nil {
		return u.result, nil
	}
	first := u.frames[0]
	return &epb.BrowserFile{ProfileId: first.ProfileId, Area: "uploads", Name: first.Name, Size: first.TotalSize, Sha256: first.Sha256}, nil
}
func (u *viewerTestUpload) Close() error { u.once.Do(func() { close(u.closed) }); return nil }

func TestViewerUploadStreamingIntegrityAndUnknownCommit(t *testing.T) {
	contents := []byte("upload contents\n")
	checksum := fileChecksum(contents)
	for _, scenario := range []struct {
		name, checksum string
		result         *epb.BrowserFile
		code           int
		finish         bool
	}{
		{"complete", checksum, nil, http.StatusOK, true},
		{"checksum mismatch", strings.Repeat("0", 64), nil, http.StatusBadRequest, false},
		{"untrusted ack", checksum, &epb.BrowserFile{ProfileId: "other", Name: "example.txt", Area: "uploads", Size: int64(len(contents)), Sha256: checksum}, http.StatusBadGateway, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			upload := &viewerTestUpload{closed: make(chan struct{}), result: scenario.result}
			w := startViewerTestWorker(t, &viewerTestBrowser{write: func(context.Context) (sdk.BrowserWriteStream, error) { return upload, nil }})
			viewer, client, csrf := bootstrapViewerTest(t, w, true)
			query := url.Values{"name": {"example.txt"}, "size": {"16"}, "sha256": {scenario.checksum}}
			response := viewerTestRequest(t, client, http.MethodPut, w.origin+"/v/"+viewer.Id+"/api/files/upload?"+query.Encode(), w.origin, csrf, contents)
			result, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != scenario.code {
				t.Fatalf("upload HTTP %d want %d: %s", response.StatusCode, scenario.code, result)
			}
			<-upload.closed
			if len(upload.frames) < 2 {
				t.Fatalf("missing upload framing: %v", upload.frames)
			}
			first := upload.frames[0]
			if first.ServerId != "server" || first.ProfileId != "profile" || first.Name != "example.txt" || first.TotalSize != int64(len(contents)) || first.Sha256 != scenario.checksum || first.RequestId == "" || first.Finish {
				t.Fatalf("upload metadata changed: %v", first)
			}
			var transferred []byte
			finished := false
			for _, frame := range upload.frames {
				if len(frame.Data) > fileChunkBytes {
					t.Fatal("unbounded upload chunk")
				}
				transferred = append(transferred, frame.Data...)
				finished = finished || frame.Finish
			}
			if !bytes.Equal(transferred, contents) || finished != scenario.finish {
				t.Fatalf("upload data/finish mismatch: %q %v", transferred, finished)
			}
			if scenario.name == "untrusted ack" && !bytes.Contains(result, []byte("outcome is unknown")) {
				t.Fatal("uncertain committed upload misreported")
			}
		})
	}
}

func TestViewerFilesRejectPathsAndChangedMetadata(t *testing.T) {
	var read bool
	w := startViewerTestWorker(t, &viewerTestBrowser{
		list: func(context.Context, *epb.BrowserFileRequest) (*epb.BrowserFileList, error) {
			return &epb.BrowserFileList{}, nil
		},
		read: func(context.Context, *epb.BrowserFileRequest) (sdk.Receiver[epb.BrowserFileChunk], error) {
			read = true
			return nil, nil
		},
	})
	viewer, client, csrf := bootstrapViewerTest(t, w, false)
	for _, scenario := range []struct {
		name string
		code int
	}{{"../outside", http.StatusBadRequest}, {"safe.txt", http.StatusConflict}} {
		body, _ := json.Marshal(map[string]any{"area": "downloads", "name": scenario.name, "size": 0, "sha256": fileChecksum(nil)})
		response := viewerTestRequest(t, client, http.MethodPost, w.origin+"/v/"+viewer.Id+"/api/files/download", w.origin, csrf, body)
		response.Body.Close()
		if response.StatusCode != scenario.code {
			t.Fatalf("file boundary accepted %q: %d", scenario.name, response.StatusCode)
		}
	}
	if read {
		t.Fatal("unapproved file version reached the download RPC")
	}
}

type viewerBlockedUpload struct {
	viewerTestUpload
	header     chan struct{}
	headerOnce sync.Once
}

func (u *viewerBlockedUpload) Send(frame *epb.BrowserFileChunk) error {
	err := u.viewerTestUpload.Send(frame)
	u.headerOnce.Do(func() { close(u.header) })
	return err
}

func TestClosingViewerInterruptsBlockedUploadAndDrainsHandlers(t *testing.T) {
	upload := &viewerBlockedUpload{viewerTestUpload: viewerTestUpload{closed: make(chan struct{})}, header: make(chan struct{})}
	w := startViewerTestWorker(t, &viewerTestBrowser{write: func(context.Context) (sdk.BrowserWriteStream, error) { return upload, nil }})
	viewer, client, csrf := bootstrapViewerTest(t, w, false)
	base, _ := url.Parse(w.origin + "/v/" + viewer.Id + "/")
	cookies := client.Jar.Cookies(base)
	if len(cookies) != 1 {
		t.Fatal("missing authenticated cookie")
	}
	connection, err := net.DialTimeout("tcp", w.host, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	query := url.Values{"name": {"blocked.txt"}, "size": {"16"}, "sha256": {fileChecksum(make([]byte, 16))}}
	// Deliberately withhold the rest of the request body. Cancellation must
	// interrupt the socket Read before attempting net/http Body.Close.
	if _, err := fmt.Fprintf(connection, "PUT /v/%s/api/files/upload?%s HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nCookie: %s\r\nX-Shiki-CSRF: %s\r\nContent-Type: application/octet-stream\r\nContent-Length: 16\r\n\r\nx", viewer.Id, query.Encode(), w.host, w.origin, cookies[0].String(), csrf); err != nil {
		t.Fatal(err)
	}
	select {
	case <-upload.header:
	case <-time.After(2 * time.Second):
		t.Fatal("upload metadata never reached the remote stream")
	}
	if _, err := w.CloseBrowserViewer(context.Background(), &epb.CloseBrowserViewerRequest{ViewerId: viewer.Id}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-upload.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("viewer cancellation left a request-body Read blocked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := w.Stop(ctx); err != nil {
		t.Fatalf("Stop did not drain canceled HTTP handlers: %v", err)
	}
}
