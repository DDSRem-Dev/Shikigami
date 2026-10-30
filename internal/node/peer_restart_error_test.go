package node

import (
	"context"
	"errors"
	"fmt"
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
	for cause := err; cause != nil && len(types) < 8; cause = errors.Unwrap(cause) {
		types = append(types, fmt.Sprintf("%T", cause))
	}
	// An actual bounded probe deadline is handled separately by the agent.
	if remote || !connection.IsRetryableNetworkError(err) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("peer shutdown lost recoverable transport cause: code=%s remote=%t types=%v", connect.CodeOf(err), remote, types)
	}
}
