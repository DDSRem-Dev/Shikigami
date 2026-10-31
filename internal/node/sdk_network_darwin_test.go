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

// A normal SDK dialer replaces the OS dialer's Control hook with its interface
// binding hook. In userspace mode it must preserve the OS dialer unchanged.
// Exercise actual socket dialing, rather than asserting the global setter ran.
func TestDarwinSDKUsesOrdinaryOSDialer(t *testing.T) {
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	mon := netmon.NewStatic()
	t.Cleanup(func() { _ = mon.Close() })
	netns.SetEnabled(true)
	t.Cleanup(func() { netns.SetEnabled(true) })
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	want := errors.New("ordinary OS dialer control reached")
	newDialer := func() netns.Dialer {
		return netns.FromDialer(logger.Discard, mon, &net.Dialer{
			Control: func(string, string, syscall.RawConn) error { return want },
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := newDialer().DialContext(ctx, "tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("baseline SDK loopback dial: %v", err)
	}
	_ = conn.Close()
	configureSDKNetworking()
	// A later control-plane netmap can update both interface knobs. Neither may
	// reinstate interface binding for this userspace-only process.
	netns.SetBindToInterfaceByRoute(logger.Discard, false)
	netns.SetDisableBindConnToInterface(logger.Discard, false)
	conn, err = newDialer().DialContext(ctx, "tcp4", listener.Addr().String())
	if conn != nil {
		_ = conn.Close()
	}
	if !errors.Is(err, want) {
		t.Fatalf("SDK replaced ordinary OS dialer control: %v", err)
	}
}
