package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
)

func bearer(values []string) (string, error) {
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer shiki_") {
		return "", ErrUnauthenticated
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	if strings.ContainsAny(token, " \t\r\n,") {
		return "", ErrUnauthenticated
	}
	return token, nil
}

// HTTPMiddleware binds the transport request to credential lifetime before
// Connect constructs its stream. Install behind the HTTP protocol admission
// layer, around the remote daemon mux only (never agent or CoreAPI listeners).
func (m *Manager) HTTPMiddleware(next http.Handler) http.Handler {
	writer := connecthttp.NewErrorWriter(connecthttp.WithRequireConnectProtocolHeader())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		raw, err := bearer(r.Header.Values("Authorization"))
		if err != nil {
			_ = writer.Write(w, r, connect.NewError(connect.CodeUnauthenticated, ErrUnauthenticated.Error()))
			return
		}
		ctx, _, release, err := m.BindContext(r.Context(), raw)
		if err != nil {
			_ = writer.Write(w, r, connect.NewError(connect.CodeUnauthenticated, ErrUnauthenticated.Error()))
			return
		}
		defer release()
		// Canceling context alone does not wake HTTP server body reads or a
		// writer blocked on HTTP/2 flow control. Deadlines apply to this stream,
		// and body close wakes its reader without closing unrelated h2 calls.
		finished := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			defer close(finished)
			controller := http.NewResponseController(w)
			_ = controller.SetReadDeadline(time.Now())
			_ = controller.SetWriteDeadline(time.Now())
			_ = r.Body.Close()
		})
		defer func() {
			if !stop() {
				<-finished
			}
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Interceptor applies the same explicit permission table to unary and all
// streaming methods before reading a business message. HTTPMiddleware supplies
// verified claims; bypassing it fails closed.
func (m *Manager) Interceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			call, ok := connect.CallInfoForServerContext(ctx)
			if !ok {
				return connect.NewError(connect.CodeUnauthenticated, ErrUnauthenticated.Error())
			}
			if _, err := bearer(call.RequestHeader().Values("Authorization")); err != nil {
				return connect.NewError(connect.CodeUnauthenticated, ErrUnauthenticated.Error())
			}
			claims, ok := FromContext(ctx)
			if !ok {
				return connect.NewError(connect.CodeUnauthenticated, ErrUnauthenticated.Error())
			}
			if err := Authorize(claims, spec.Procedure); err != nil {
				return connect.NewError(connect.CodePermissionDenied, ErrPermissionDenied.Error())
			}
			err := next(ctx, spec, stream)
			if cause := context.Cause(ctx); errors.Is(cause, ErrRevoked) || errors.Is(cause, ErrExpired) {
				return connect.NewError(connect.CodeUnauthenticated, ErrUnauthenticated.Error())
			}
			return err
		}
	}
}
