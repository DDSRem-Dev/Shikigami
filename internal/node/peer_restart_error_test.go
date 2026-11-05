package node

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestSDKPeerShutdownErrorRetainsNetworkCause(t *testing.T) {
	ctx, _, right, _, client := connectedNodePair(t)
	if err := right.Close(); err != nil {
		t.Fatal(err)
	}
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := client.NegotiateProtocol(probe, &pb.ProtocolOffer{Major: 1})
	if err == nil {
		t.Fatal("stopped peer still admitted a new protocol probe")
	}
	var ce *connect.Error
	remote := errors.As(err, &ce) && ce.IsRemote()
	var types []string
	causes := []error{err}
	for len(causes) > 0 && len(types) < 24 {
		cause := causes[0]
		causes = causes[1:]
		if cause == nil {
			continue
		}
		types = append(types, fmt.Sprintf("%T", cause))
		switch wrapped := cause.(type) {
		case interface{ Unwrap() []error }:
			causes = append(causes, wrapped.Unwrap()...)
		case interface{ Unwrap() error }:
			causes = append(causes, wrapped.Unwrap())
		}
	}
	// An actual bounded probe deadline is handled separately by the agent.
	if remote || !connection.IsRetryableNetworkError(err) && !errors.Is(err, context.DeadlineExceeded) {
		var network net.Error
		t.Fatalf("peer shutdown lost recoverable transport cause: code=%s remote=%t types=%v caller_done=%t canceled=%t deadline=%t reset=%t pipe=%t EOF=%t unexpected_EOF=%t network=%t", connect.CodeOf(err), remote, types, probe.Err() != nil, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.As(err, &network))
	}
}
