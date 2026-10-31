//go:build darwin

package node

import "tailscale.com/net/netns"

// configureSDKNetworking follows tailscaled's userspace-networking policy.
// Shiki never supplies a kernel TUN to tsnet or installs system routes, so its
// underlay sockets must follow the user's OS routes, including VPN/fake-IP DNS
// routes. Binding them to the physical default interface can make those DNS
// answers unreachable. This process-wide SDK setting affects all underlay
// sockets; application traffic still enters our authorized tsnet netstack.
// Unlike the per-interface debug knobs, netmap updates do not reset this policy.
func configureSDKNetworking() {
	netns.SetEnabled(false)
}
