package client

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

// The noVNC tree is unmodified upstream source, pinned with per-file digests in
// web/novnc/UPSTREAM.json. No browser resource is fetched from a CDN.
//
//go:embed web
var webFiles embed.FS

func singleHeader(r *http.Request, key string) string {
	values := r.Header.Values(key)
	if len(values) != 1 {
		return ""
	}
	return values[0]
}

func (w *Worker) serveHTTP(out http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	if w.closed || w.lifetime == nil || w.lifetime.Err() != nil {
		w.mu.Unlock()
		http.Error(out, "Browser viewer is not running", http.StatusServiceUnavailable)
		return
	}
	// Admission and Stop's closed transition share this lock. The wait group
	// covers ordinary HTTP transfers and hijacked WebSockets alike.
	w.requests.Add(1)
	w.mu.Unlock()
	defer w.requests.Done()
	out.Header().Set("Cache-Control", "no-store")
	out.Header().Set("Referrer-Policy", "no-referrer")
	out.Header().Set("X-Content-Type-Options", "nosniff")
	out.Header().Set("X-Frame-Options", "DENY")
	out.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	out.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	out.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), usb=(), serial=(), publickey-credentials-get=()")
	out.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' ws://"+w.host+"; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() || r.Host != w.host || r.URL.IsAbs() || strings.Contains(r.URL.EscapedPath(), "%") || strings.TrimSuffix(r.URL.Path, "/") != strings.TrimSuffix(path.Clean(r.URL.Path), "/") {
		http.Error(out, "Loopback viewer host required", http.StatusForbidden)
		return
	}
	// A top-level bootstrap navigation may originate from a terminal/web UI.
	// It exposes no session state; only the subsequent same-origin, one-use
	// POST can redeem the fragment credential.
	bootstrapNavigation := r.Method == http.MethodGet && r.URL.Path == "/" && r.URL.RawQuery == "" && r.Header.Get("Sec-Fetch-Mode") == "navigate"
	// Fetch Metadata supplements, but does not replace, Origin and CSRF checks.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" && !bootstrapNavigation {
		http.Error(out, "Cross-origin viewer access denied", http.StatusForbidden)
		return
	}
	if origins := r.Header.Values("Origin"); len(origins) != 0 && (len(origins) != 1 || origins[0] != w.origin) {
		http.Error(out, "Viewer origin mismatch", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/" && r.Method == http.MethodGet && r.URL.RawQuery == "" {
		w.serveAsset(out, r, "bootstrap.html")
		return
	}
	if r.URL.Path == "/bootstrap.js" && r.Method == http.MethodGet && r.URL.RawQuery == "" {
		w.serveAsset(out, r, "bootstrap.js")
		return
	}
	if r.URL.Path == "/bootstrap" && r.Method == http.MethodPost && r.URL.RawQuery == "" {
		w.handleBootstrap(out, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") && r.Method == http.MethodGet && r.URL.RawQuery == "" {
		asset := strings.TrimPrefix(r.URL.Path, "/assets/")
		if strings.HasSuffix(asset, "/") || asset == "" || path.Clean(asset) != asset {
			http.NotFound(out, r)
			return
		}
		w.serveAsset(out, r, asset)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
	if len(parts) != 3 || parts[0] != "v" || parts[1] == "" {
		http.NotFound(out, r)
		return
	}
	v := w.authenticatedViewer(r, parts[1])
	if v == nil {
		http.Error(out, "Viewer session expired or invalid; open a new viewer from shiki", http.StatusUnauthorized)
		return
	}
	endpoint := parts[2]
	if endpoint == "" && r.Method == http.MethodGet && r.URL.RawQuery == "" {
		w.serveAsset(out, r, "viewer.html")
		return
	}
	if endpoint == "rfb" && r.Method == http.MethodGet && r.URL.RawQuery == "" {
		w.handleRFB(out, r, v)
		return
	}
	if endpoint == "api/session" && r.Method == http.MethodGet && r.URL.RawQuery == "" {
		w.handleSession(out, r, v)
		return
	}
	if !w.validCSRF(r, v) {
		http.Error(out, "Viewer CSRF validation failed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && singleHeader(r, "Origin") != w.origin {
		http.Error(out, "Viewer origin required", http.StatusForbidden)
		return
	}
	switch {
	case endpoint == "api/status" && r.Method == http.MethodGet && r.URL.RawQuery == "":
		w.handleStatus(out, r, v)
	case endpoint == "api/control" && r.Method == http.MethodPost && r.URL.RawQuery == "":
		w.handleControl(out, r, v)
	case endpoint == "api/files" && r.Method == http.MethodGet:
		w.handleFileList(out, r, v)
	case endpoint == "api/files/upload" && r.Method == http.MethodPut:
		w.handleUpload(out, r, v)
	case endpoint == "api/files/download" && r.Method == http.MethodPost && r.URL.RawQuery == "":
		w.handleDownload(out, r, v)
	case endpoint == "api/close" && r.Method == http.MethodPost && r.URL.RawQuery == "":
		w.mu.Lock()
		if w.viewers[v.id] == v {
			w.removeViewerLocked(v)
		}
		w.mu.Unlock()
		http.SetCookie(out, &http.Cookie{Name: cookieName(v), Path: viewerPath(v), MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writeJSON(out, http.StatusOK, map[string]string{"state": "closed"})
	default:
		http.NotFound(out, r)
	}
}

func (w *Worker) serveAsset(out http.ResponseWriter, r *http.Request, asset string) {
	file, err := webFiles.Open("web/" + asset)
	if err != nil {
		http.NotFound(out, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(out, r)
		return
	}
	reader, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(out, "Embedded viewer resource unavailable", http.StatusInternalServerError)
		return
	}
	switch path.Ext(asset) {
	case ".js":
		out.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case ".css":
		out.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".html":
		out.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".json":
		out.Header().Set("Content-Type", "application/json")
	default:
		out.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	http.ServeContent(out, r, asset, time.Time{}, reader)
}

func viewerPath(v *viewer) string { return "/v/" + v.id + "/" }
func cookieName(v *viewer) string { return "shiki_viewer_" + v.id }

func (w *Worker) handleBootstrap(out http.ResponseWriter, r *http.Request) {
	if singleHeader(r, "Origin") != w.origin || singleHeader(r, "X-Shiki-Viewer") != "bootstrap" {
		http.Error(out, "Viewer bootstrap origin required", http.StatusForbidden)
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	if !decodeJSON(out, r, &request) {
		return
	}
	if len(request.Token) != 43 {
		http.Error(out, "Invalid or expired one-use viewer URL", http.StatusUnauthorized)
		return
	}
	cookie, err := randomSecret(32)
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	key := sha256.Sum256([]byte(request.Token))
	w.mu.Lock()
	v := w.viewers[w.bootstrap[key]]
	if w.closed || v == nil || v.bootstrapped || time.Now().After(v.expires) || v.ctx.Err() != nil {
		w.mu.Unlock()
		http.Error(out, "Invalid or expired one-use viewer URL; request another from shiki", http.StatusUnauthorized)
		return
	}
	delete(w.bootstrap, key)
	v.bootstrapped, v.cookieHash, v.expires = true, sha256.Sum256([]byte(cookie)), time.Now().Add(w.config.sessionTTL)
	if v.timer != nil {
		v.timer.Stop()
	}
	w.armExpiryLocked(v)
	expires := v.expires
	w.mu.Unlock()
	// HTTP is intentionally limited to loopback. The path and random cookie
	// isolate viewer sessions; the daemon/CoreAPI credential is never sent here.
	http.SetCookie(out, &http.Cookie{Name: cookieName(v), Value: cookie, Path: viewerPath(v), HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
	writeJSON(out, http.StatusOK, map[string]any{"path": viewerPath(v), "expiresAt": expires.Unix()})
}

func (w *Worker) authenticatedViewer(r *http.Request, id string) *viewer {
	w.mu.Lock()
	defer w.mu.Unlock()
	v := w.viewers[id]
	if w.closed || v == nil || !v.bootstrapped || time.Now().After(v.expires) || v.ctx.Err() != nil {
		return nil
	}
	var value string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == cookieName(v) {
			value = cookie.Value
			count++
		}
	}
	if count != 1 || len(value) != 43 {
		return nil
	}
	actual := sha256.Sum256([]byte(value))
	if subtle.ConstantTimeCompare(actual[:], v.cookieHash[:]) != 1 {
		return nil
	}
	return v
}

func (w *Worker) validCSRF(r *http.Request, v *viewer) bool {
	return subtle.ConstantTimeCompare([]byte(singleHeader(r, "X-Shiki-CSRF")), []byte(v.csrf)) == 1
}

func (w *Worker) handleSession(out http.ResponseWriter, _ *http.Request, v *viewer) {
	w.mu.Lock()
	expires := v.expires.Unix()
	w.mu.Unlock()
	writeJSON(out, http.StatusOK, map[string]any{"id": v.id, "server": v.serverID, "profile": v.profileID, "readOnly": v.readOnly, "csrf": v.csrf, "expiresAt": expires, "maxFileBytes": w.config.maxFileBytes})
}

func viewerRequestContext(r *http.Request, v *viewer, budget time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	stop := context.AfterFunc(v.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

func (w *Worker) handleStatus(out http.ResponseWriter, r *http.Request, v *viewer) {
	ctx, cancel := viewerRequestContext(r, v, w.config.operationTimeout)
	defer cancel()
	status, err := w.remote.GetBrowserStatus(ctx, &epb.BrowserScope{ServerId: v.serverID, ProfileId: v.profileID})
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	if status == nil {
		writeHTTPError(out, errors.New("empty browser status"))
		return
	}
	writeJSON(out, http.StatusOK, status)
}

func (w *Worker) handleControl(out http.ResponseWriter, r *http.Request, v *viewer) {
	if v.readOnly {
		writeHTTPError(out, connect.NewError(connect.CodePermissionDenied, "VIEWER_READ_ONLY"))
		return
	}
	var request struct {
		Revision   string `json:"revision"`
		Attachment string `json:"attachment"`
		Instance   string `json:"instance"`
	}
	if !decodeJSON(out, r, &request) {
		return
	}
	if request.Revision == "" || len(request.Revision) > 256 || request.Attachment == "" || len(request.Attachment) > 128 || request.Instance == "" || len(request.Instance) > 128 {
		writeHTTPError(out, connect.NewError(connect.CodeInvalidArgument, "CONTROL_SCOPE_REQUIRED"))
		return
	}
	v.mu.Lock()
	connection := v.connection
	if connection == nil || connection.id == "" || !connection.canControl || connection.ctx.Err() != nil {
		v.mu.Unlock()
		writeHTTPError(out, connect.NewError(connect.CodeFailedPrecondition, "VIEWER_NOT_ATTACHED"))
		return
	}
	// The page confirms a specific attachment. A delayed request from an old
	// page generation must never inherit the replacement connection's scope.
	if request.Attachment != connection.id || request.Instance != connection.instance {
		v.mu.Unlock()
		writeHTTPError(out, connect.NewError(connect.CodeAborted, "VIEWER_ATTACHMENT_CHANGED"))
		return
	}
	id, instance := connection.id, connection.instance
	v.mu.Unlock()
	ctx, cancel := viewerRequestContext(r, v, w.config.operationTimeout)
	defer cancel()
	stop := context.AfterFunc(connection.ctx, cancel)
	defer stop()
	status, err := w.remote.TakeBrowserControl(ctx, &epb.BrowserControlRequest{ServerId: v.serverID, ProfileId: v.profileID, InstanceId: instance, AttachmentId: id, ExpectedControlRevision: request.Revision})
	if err != nil {
		writeHTTPError(out, err)
		return
	}
	if status == nil {
		writeHTTPError(out, errors.New("empty takeover status"))
		return
	}
	writeJSON(out, http.StatusOK, status)
}

func decodeJSON(out http.ResponseWriter, r *http.Request, target any) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		http.Error(out, "JSON request required", http.StatusUnsupportedMediaType)
		return false
	}
	controller := http.NewResponseController(out)
	controller.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	r.Body = http.MaxBytesReader(out, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(out, "Invalid viewer request", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(out, "One viewer request required", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(out http.ResponseWriter, status int, value any) {
	out.Header().Set("Content-Type", "application/json")
	out.WriteHeader(status)
	json.NewEncoder(out).Encode(value)
}

type viewerError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func safeError(err error) (int, viewerError) {
	code := connect.CodeOf(err)
	if errors.Is(err, context.Canceled) {
		code = connect.CodeCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = connect.CodeDeadlineExceeded
	}
	status, message := http.StatusInternalServerError, "Unknown browser failure. Refresh status before trying again; input is never replayed."
	switch code {
	case connect.CodeCanceled:
		status, message = http.StatusRequestTimeout, "The browser operation was canceled."
	case connect.CodeDeadlineExceeded:
		status, message = http.StatusGatewayTimeout, "The browser operation timed out; its outcome may be unknown. Refresh status before retrying."
	case connect.CodeInvalidArgument:
		status, message = http.StatusBadRequest, "The browser request was invalid. Check the file name, size, or current attachment."
	case connect.CodeUnauthenticated:
		status, message = http.StatusUnauthorized, "The viewer or remote authorization expired. Open a new viewer from shiki."
	case connect.CodePermissionDenied:
		status, message = http.StatusForbidden, "Permission denied. Control and file permissions are enforced on the server."
	case connect.CodeNotFound:
		status, message = http.StatusNotFound, "The profile, attachment, or staging file no longer exists."
	case connect.CodeAlreadyExists:
		status, message = http.StatusConflict, "That resource already exists. Refresh the staging file list before retrying."
	case connect.CodeAborted:
		status, message = http.StatusConflict, "The control revision or file changed. Refresh status and explicitly confirm the action again."
	case connect.CodeFailedPrecondition:
		status, message = http.StatusConflict, "The browser is not running, the attachment ended, or this action is not currently available."
	case connect.CodeResourceExhausted:
		status, message = http.StatusTooManyRequests, "A browser viewer, transfer, or data limit was reached."
	case connect.CodeUnavailable:
		status, message = http.StatusServiceUnavailable, "The remote browser service is unavailable. The independent browser may still be running."
	case connect.CodeDataLoss:
		status, message = http.StatusBadGateway, "The file or screen stream was incomplete or failed integrity validation. Nothing incomplete was downloaded."
	}
	return status, viewerError{Code: code.String(), Message: message}
}

func writeHTTPError(out http.ResponseWriter, err error) {
	status, body := safeError(err)
	writeJSON(out, status, body)
}
