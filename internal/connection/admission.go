package connection

import (
	"mime"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
)

// NewRPCHandler mounts Connect with a protocol allowlist outside its handlers.
// The allowlist is derived from registered specs, so streaming content types
// cannot be used to bypass the unary/stream boundary.
func NewRPCHandler(server *connect.Server, middleware ...func(http.Handler) http.Handler) http.Handler {
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server, connecthttp.WithRequireConnectProtocolHeader(),
		connecthttp.WithCompressors(), connecthttp.WithReadMaxBytes(MaxMessageBytes), connecthttp.WithSendMaxBytes(MaxMessageBytes))
	var handler http.Handler = mux
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	methods := map[string]connect.StreamType{}
	for spec := range server.Specs() {
		methods[spec.Procedure] = spec.StreamType
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.ProtoMajor != 2 {
			http.Error(w, "HTTP2_REQUIRED", http.StatusHTTPVersionNotSupported)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST_REQUIRED", http.StatusMethodNotAllowed)
			return
		}
		if values := r.Header.Values("Connect-Protocol-Version"); len(values) != 1 || values[0] != "1" {
			http.Error(w, "CONNECT_VERSION_REQUIRED", http.StatusBadRequest)
			return
		}
		values := r.Header.Values("Content-Type")
		if len(values) != 1 {
			http.Error(w, "PROTOBUF_REQUIRED", http.StatusUnsupportedMediaType)
			return
		}
		media, params, err := mime.ParseMediaType(values[0])
		if err != nil || len(params) != 0 || (media != "application/proto" && media != "application/connect+proto") {
			http.Error(w, "PROTOBUF_REQUIRED", http.StatusUnsupportedMediaType)
			return
		}
		if kind, ok := methods[r.URL.Path]; ok && ((kind == connect.StreamTypeUnary) != (media == "application/proto")) {
			http.Error(w, "RPC_CONTENT_TYPE_MISMATCH", http.StatusUnsupportedMediaType)
			return
		}
		handler.ServeHTTP(w, r)
	})
}
