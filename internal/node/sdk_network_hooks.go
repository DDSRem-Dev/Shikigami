package node

import "tailscale.com/feature"

// A missing SDK feature hook would silently bypass the proxy resolver even
// when its configuration succeeds. Refuse node startup before any dialing.
func requireSDKProxyHooks() error {
	_, resolve := feature.HookProxyFromEnvironment.GetOk()
	_, invalidate := feature.HookProxyInvalidateCache.GetOk()
	_, auth := feature.HookProxyGetAuthHeader.GetOk()
	_, self := feature.HookProxySetSelfProxy.GetOk()
	_, connect := feature.HookProxySetTransportGetProxyConnectHeader.GetOk()
	if !resolve || !invalidate || !auth || !self || !connect {
		return errSDKProxyInitialization
	}
	return nil
}
