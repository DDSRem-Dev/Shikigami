package connection

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/url"
	"syscall"

	"connectrpc.com/connect/v2"
	rpcerrors "github.com/DDSRem-Dev/Shikigami/pkg/errors"
)

// RebuildError is shared with the standalone plugin SDK.
func RebuildError(err error) error { return rpcerrors.RebuildError(err) }

// IsRetryableNetworkError deliberately excludes remote application errors,
// certificate failures and local cancellation. It must not be used to replay
// terminal input, TCP data, or an uncertain mutation.
func IsRetryableNetworkError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrHTTP2Required) {
		return false
	}
	var verification *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &verification) || errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return false
	}
	var ce *connect.Error
	if errors.As(err, &ce) {
		if ce.IsRemote() {
			return false
		}
		// Connect v2 maps local ECONNRESET/EPIPE to Canceled. Those are
		// transport failures, while an actual context cancellation above or a
		// remotely supplied Canceled status must never trigger retry/fallback.
		if ce.Code() == connect.CodeCanceled && (errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)) {
			return true
		}
		if ce.Code() != connect.CodeUnavailable {
			return false
		}
	}
	// url.Error implements net.Error even when its cause is a rejected redirect
	// or a local policy error. Classify its cause rather than the wrapper.
	var ue *url.Error
	if errors.As(err, &ue) {
		return IsRetryableNetworkError(ue.Err)
	}
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}
