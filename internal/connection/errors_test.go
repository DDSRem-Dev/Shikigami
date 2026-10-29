package connection

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"

	"connectrpc.com/connect/v2"
)

func TestConnectCanceledNetworkResetDoesNotMeanCallerCancellation(t *testing.T) {
	reset := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"local reset", connect.NewError(connect.CodeCanceled, "reset").WithCause(reset), true},
		{"local broken pipe", connect.NewError(connect.CodeCanceled, "pipe").WithCause(syscall.EPIPE), true},
		{"remote canceled", connect.NewError(connect.CodeCanceled, "reset").WithCause(reset).WithRemote(), false},
		{"caller canceled", errors.Join(context.Canceled, reset), false},
		{"caller deadline", errors.Join(context.DeadlineExceeded, reset), false},
		{"unexplained canceled", connect.NewError(connect.CodeCanceled, "canceled"), false},
		{"authentication failure", connect.NewError(connect.CodeUnauthenticated, "denied").WithCause(reset), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsRetryableNetworkError(test.err); got != test.want {
				t.Fatalf("retryable=%t, want %t", got, test.want)
			}
		})
	}
}
