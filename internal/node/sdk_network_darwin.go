//go:build darwin

package node

import (
	"context"
	"sync"
	"time"

	"golang.org/x/net/http/httpproxy"
	"tailscale.com/net/tshttpproxy"
)

var sdkProxyOnce sync.Once
var sdkProxyError error

// configureSDKNetworking preserves the SDK's physical-interface policy for
// WireGuard/UDP. Only its HTTP(S) proxy resolver adopts the user's existing
// static macOS proxy settings. The immutable process snapshot is shared by all
// embedded nodes; restarting Shiki is required to read changed proxy settings.
func configureSDKNetworking() error {
	sdkProxyOnce.Do(func() {
		resolver, err := systemProxyResolver(httpproxy.FromEnvironment(), func() ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return readProxyCommand(ctx, "/usr/sbin/scutil", "--proxy")
		})
		if err != nil {
			sdkProxyError = err
			return
		}
		if err := tshttpproxy.SetProxyFunc(resolver); err != nil {
			sdkProxyError = errSDKProxyInitialization
		}
	})
	return sdkProxyError
}
