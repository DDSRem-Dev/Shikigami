//go:build darwin

package node

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"

	"tailscale.com/net/netmon"
	"tailscale.com/net/netns"
	"tailscale.com/types/logger"
)

// The HTTP proxy adapter must not change the SDK's underlay interface policy.
// Exercise a real SDK socket, whose interface Control replaces the sentinel;
// SetEnabled(false) would preserve the sentinel and fail this dial instead.
func TestDarwinProxyPreservesSDKInterfacePolicy(t *testing.T) {
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	netns.SetEnabled(true)
	t.Cleanup(func() { netns.SetEnabled(true) })
	if err := configureSDKNetworking(); err != nil {
		t.Fatal(err)
	}
	mon := netmon.NewStatic()
	t.Cleanup(func() { _ = mon.Close() })
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	blocked := errors.New("ordinary OS dialer sentinel")
	dialer := netns.FromDialer(logger.Discard, mon, &net.Dialer{Control: func(string, string, syscall.RawConn) error { return blocked }})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("SDK interface policy was disabled: %v", err)
	}
	_ = conn.Close()
}
