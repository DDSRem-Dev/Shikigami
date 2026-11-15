package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/types/known/structpb"
)

type viewerTestCore struct {
	sdk.CoreAPI
	browser sdk.BrowserClient
}

func (c viewerTestCore) Browser() sdk.BrowserClient { return c.browser }
func (viewerTestCore) Region() sdk.RegionClient     { return nil }

type viewerTestBrowser struct {
	sdk.BrowserClient
	controlMu sync.RWMutex
	status    *epb.BrowserStatus
	stream    func(context.Context) (sdk.BrowserClientStream, error)
	list      func(context.Context, *epb.BrowserFileRequest) (*epb.BrowserFileList, error)
	read      func(context.Context, *epb.BrowserFileRequest) (sdk.Receiver[epb.BrowserFileChunk], error)
	write     func(context.Context) (sdk.BrowserWriteStream, error)
	control   func(context.Context, *epb.BrowserControlRequest) (*epb.BrowserStatus, error)
}

func (b *viewerTestBrowser) GetBrowserStatus(_ context.Context, scope *epb.BrowserScope) (*epb.BrowserStatus, error) {
	if scope.ServerId != "server" || scope.ProfileId != "profile" {
		return nil, connect.NewError(connect.CodePermissionDenied, "wrong scope")
	}
	if b.status != nil {
		return b.status, nil
	}
	return &epb.BrowserStatus{ProfileId: "profile", InstanceId: "instance", State: "running", ControlRevision: "revision-1"}, nil
}
func (b *viewerTestBrowser) BrowserStream(ctx context.Context) (sdk.BrowserClientStream, error) {
	return b.stream(ctx)
}
func (b *viewerTestBrowser) ListBrowserFiles(ctx context.Context, r *epb.BrowserFileRequest) (*epb.BrowserFileList, error) {
	return b.list(ctx, r)
}
func (b *viewerTestBrowser) ReadBrowserFile(ctx context.Context, r *epb.BrowserFileRequest) (sdk.Receiver[epb.BrowserFileChunk], error) {
	return b.read(ctx, r)
}
func (b *viewerTestBrowser) WriteBrowserFile(ctx context.Context) (sdk.BrowserWriteStream, error) {
	return b.write(ctx)
}
func (b *viewerTestBrowser) TakeBrowserControl(ctx context.Context, r *epb.BrowserControlRequest) (*epb.BrowserStatus, error) {
	b.controlMu.RLock()
	callback := b.control
	b.controlMu.RUnlock()
	return callback(ctx, r)
}

func startViewerTestWorker(t *testing.T, browser sdk.BrowserClient) *Worker {
	t.Helper()
	w := New()
	if err := w.Init(context.Background(), sdk.InitContext{Side: "client", StateDir: t.TempDir(), Core: viewerTestCore{browser: browser}}); err != nil {
		t.Fatal(err)
	}
	lifetime, cancel := context.WithCancel(context.Background())
	if err := w.Start(lifetime); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := w.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return w
}

func viewerTestRequest(t *testing.T, client *http.Client, method, target, origin, csrf string, body []byte) *http.Response {
	t.Helper()
	r, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if csrf != "" {
		r.Header.Set("X-Shiki-CSRF", csrf)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func bootstrapViewerTest(t *testing.T, w *Worker, readOnly bool) (*epb.BrowserViewer, *http.Client, string) {
	t.Helper()
	launch, cancel := context.WithCancel(context.Background())
	result, err := w.OpenBrowserViewer(launch, &epb.OpenBrowserViewerRequest{ServerId: "server", ProfileId: "profile", ReadOnly: readOnly})
	cancel() // A launching CLI's cancellation must not own the viewer session.
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(result.Url)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != w.host || parsed.Scheme != "http" || parsed.RawQuery != "" || len(parsed.Fragment) != 43 || !strings.HasPrefix(parsed.Host, "127.0.0.1:") {
		t.Fatalf("unsafe viewer URL: %q", result.Url)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	body, _ := json.Marshal(map[string]string{"token": parsed.Fragment})
	request, _ := http.NewRequest(http.MethodPost, w.origin+"/bootstrap", bytes.NewReader(body))
	request.Header.Set("Origin", w.origin)
	request.Header.Set("X-Shiki-Viewer", "bootstrap")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("bootstrap %d: %s", response.StatusCode, data)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/v/"+result.Id+"/" || len(cookies[0].Value) != 43 {
		t.Fatalf("unsafe viewer cookie: %#v", cookies)
	}
	session := viewerTestRequest(t, client, http.MethodGet, w.origin+"/v/"+result.Id+"/api/session", "", "", nil)
	defer session.Body.Close()
	if session.StatusCode != http.StatusOK {
		t.Fatalf("session HTTP %d after CLI close", session.StatusCode)
	}
	var credentials struct {
		CSRF string `json:"csrf"`
	}
	if err := json.NewDecoder(session.Body).Decode(&credentials); err != nil {
		t.Fatal(err)
	}
	if len(credentials.CSRF) != 43 {
		t.Fatal("missing per-session CSRF credential")
	}
	// A successful bootstrap is one use, even from the same authenticated client.
	replay, _ := http.NewRequest(http.MethodPost, w.origin+"/bootstrap", bytes.NewReader(body))
	replay.Header = request.Header.Clone()
	second, err := client.Do(replay)
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bootstrap replay accepted: %d", second.StatusCode)
	}
	return result, client, credentials.CSRF
}

func TestViewerBootstrapBoundaryAndCLILifetime(t *testing.T) {
	w := startViewerTestWorker(t, &viewerTestBrowser{})
	viewer, client, csrf := bootstrapViewerTest(t, w, false)
	base := w.origin + "/v/" + viewer.Id + "/"
	response := viewerTestRequest(t, client, http.MethodGet, base, "", "", nil)
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Contains(page, []byte("/assets/viewer.js")) {
		t.Fatalf("viewer unavailable after CLI close: %d %v", response.StatusCode, err)
	}
	if !strings.Contains(response.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") || response.Header.Get("Referrer-Policy") != "no-referrer" || response.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("viewer security headers missing")
	}
	for _, navigation := range []struct {
		mode string
		code int
	}{{"navigate", http.StatusOK}, {"no-cors", http.StatusForbidden}} {
		request, _ := http.NewRequest(http.MethodGet, w.origin+"/", nil)
		request.Header.Set("Sec-Fetch-Site", "cross-site")
		request.Header.Set("Sec-Fetch-Mode", navigation.mode)
		navigated, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		navigated.Body.Close()
		if navigated.StatusCode != navigation.code {
			t.Fatalf("bootstrap navigation mode %q: %d", navigation.mode, navigated.StatusCode)
		}
	}
	for _, scenario := range []struct{ name, origin, token string }{{"missing csrf", w.origin, ""}, {"wrong csrf", w.origin, "wrong"}, {"wrong origin", "http://evil.invalid", csrf}, {"missing origin", "", csrf}} {
		t.Run(scenario.name, func(t *testing.T) {
			r := viewerTestRequest(t, client, http.MethodPost, base+"api/control", scenario.origin, scenario.token, []byte(`{"revision":"revision-1"}`))
			r.Body.Close()
			if r.StatusCode != http.StatusForbidden {
				t.Fatalf("boundary accepted: %d", r.StatusCode)
			}
		})
	}
	hostRequest, _ := http.NewRequest(http.MethodGet, base, nil)
	hostRequest.Host = "attacker.invalid"
	hostResponse, err := client.Do(hostRequest)
	if err != nil {
		t.Fatal(err)
	}
	hostResponse.Body.Close()
	if hostResponse.StatusCode != http.StatusForbidden {
		t.Fatal("Host rebinding accepted")
	}
	anonymous := viewerTestRequest(t, &http.Client{Timeout: 5 * time.Second}, http.MethodGet, base+"api/session", "", "", nil)
	anonymous.Body.Close()
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatal("viewer ID treated as authentication")
	}
	for _, resource := range []string{"novnc/core/rfb.js", "novnc/vendor/pako/lib/zlib/inflate.js", "novnc/docs/LICENSE.MPL-2.0", "novnc/vendor/pako/LICENSE", "novnc/UPSTREAM.json"} {
		r := viewerTestRequest(t, client, http.MethodGet, w.origin+"/assets/"+resource, "", "", nil)
		data, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != http.StatusOK || len(data) < 100 {
			t.Fatalf("embedded upstream resource missing: %s", resource)
		}
	}
	closed := viewerTestRequest(t, client, http.MethodPost, base+"api/close", w.origin, csrf, nil)
	closed.Body.Close()
	if closed.StatusCode != http.StatusOK {
		t.Fatalf("close: %d", closed.StatusCode)
	}
	after := viewerTestRequest(t, client, http.MethodGet, base, "", "", nil)
	after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Fatal("closed viewer still authorized")
	}
}

func TestViewerExpiryReadOnlyAndNotRunning(t *testing.T) {
	w := startViewerTestWorker(t, &viewerTestBrowser{})
	result, client, csrf := bootstrapViewerTest(t, w, true)
	base := w.origin + "/v/" + result.Id + "/"
	response := viewerTestRequest(t, client, http.MethodPost, base+"api/control", w.origin, csrf, []byte(`{"revision":"revision-1"}`))
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("immutable read-only session allowed control")
	}
	w.mu.Lock()
	w.viewers[result.Id].expires = time.Now().Add(-time.Second)
	w.mu.Unlock()
	expired := viewerTestRequest(t, client, http.MethodGet, base+"api/session", "", "", nil)
	expired.Body.Close()
	if expired.StatusCode != http.StatusUnauthorized {
		t.Fatal("expired session authorized")
	}
	stopped := startViewerTestWorker(t, &viewerTestBrowser{status: &epb.BrowserStatus{State: "stopped"}})
	if _, err := stopped.OpenBrowserViewer(context.Background(), &epb.OpenBrowserViewerRequest{ServerId: "server", ProfileId: "profile"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("stopped browser silently started or accepted: %v", err)
	}
}

func TestViewerConfigBounds(t *testing.T) {
	for _, values := range []map[string]any{{"viewer_max_sessions": 0}, {"viewer_bootstrap_seconds": 601}, {"viewer_session_seconds": 299}, {"viewer_max_file_bytes": float64(1<<30) + 1}, {"viewer_operation_seconds": 4}, {"viewer_max_sessions": 1.5}, {"viewer_max_sessions": "8"}} {
		config, err := structpb.NewStruct(values)
		if err != nil {
			t.Fatal(err)
		}
		if ValidateConfig(config) == nil {
			t.Fatalf("invalid viewer config accepted: %v", values)
		}
	}
	config, _ := structpb.NewStruct(map[string]any{"viewer_max_sessions": 4, "max_file_size_mb": 256})
	if err := ValidateConfig(config); err != nil {
		t.Fatalf("server settings rejected by viewer: %v", err)
	}
}
