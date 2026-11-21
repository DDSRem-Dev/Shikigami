package client

import (
	"context"
	"fmt"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
)

// Region and Browser use the agent's authenticated remote routing. They never
// connect to a daemon or start a plugin themselves. Remote messages must also
// name serverID; the agent rejects a mismatch with the routing header.
func (c *Client) Region(serverID string) erpc.RegionServiceClient {
	return erpc.NewRegionServiceClient(connection.NewRPCClient(c.endpoint, c.extensionIdentity(serverID, "region.host.v1")))
}

func (c *Client) Browser(serverID string) erpc.BrowserServiceClient {
	return erpc.NewBrowserServiceClient(connection.NewRPCClient(c.endpoint, c.extensionIdentity(serverID, "browser.remote.v1")))
}

// BrowserLocal routes only to the local viewer plugin. The requested remote
// server belongs in OpenBrowserViewerRequest, never in the local routing header.
func (c *Client) BrowserLocal() erpc.BrowserLocalServiceClient {
	return erpc.NewBrowserLocalServiceClient(connection.NewRPCClient(c.endpoint, c.extensionIdentity("", "browser.viewer.v1")))
}

func (c *Client) extensionIdentity(serverID, capability string) connect.ClientInterceptor {
	return func(next connect.ClientFunc) connect.ClientFunc {
		identified := c.identity(serverID)(next)
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			required := capability
			if spec.Procedure == erpc.BrowserServiceStartBrowserProcedure {
				required = "browser.launch-mode.v1"
			}
			if !slices.Contains(c.capabilities, capability) {
				required = capability
			}
			if !slices.Contains(c.capabilities, required) {
				return nil, connect.NewError(connect.CodeUnimplemented, fmt.Sprintf("agent did not negotiate %s; update the agent and enable the required plugin (the local browser plugin is required for viewers)", required))
			}
			if capability != "browser.viewer.v1" && serverID == "" {
				return nil, connect.NewError(connect.CodeInvalidArgument, "remote extension calls require an explicit resolved server ID")
			}
			return identified(ctx, spec)
		}
	}
}

// Extension operations have bounded server work longer than the legacy short
// IPC budget. An explicit non-default timeout and any caller deadline still win.
func extensionProcedureTimeout(procedure string) time.Duration {
	switch procedure {
	case erpc.RegionServiceApplyRegionProcedure, erpc.RegionServiceRollbackRegionProcedure:
		return 90 * time.Second
	case erpc.RegionServiceDiagnoseRegionProcedure:
		return 40 * time.Second
	case erpc.BrowserServiceStartBrowserProcedure, erpc.BrowserServiceStopBrowserProcedure, erpc.BrowserServiceDeleteBrowserProfileProcedure:
		// Startup may wait for the administrator-unlocked keyring; stop/delete
		// also need enough time when queued behind that bounded startup.
		return 120 * time.Second
	case erpc.BrowserServiceDiagnoseBrowserProcedure:
		return 90 * time.Second
	case erpc.RegionServiceInspectRegionProcedure, erpc.RegionServicePlanRegionProcedure, erpc.RegionServiceConfirmRegionProcedure, erpc.RegionServiceGetRegionOperationProcedure,
		erpc.BrowserServiceListBrowserProfilesProcedure, erpc.BrowserServicePutBrowserProfileProcedure, erpc.BrowserServiceGetBrowserStatusProcedure, erpc.BrowserServiceTakeBrowserControlProcedure, erpc.BrowserServiceListBrowserFilesProcedure,
		erpc.BrowserLocalServiceOpenBrowserViewerProcedure, erpc.BrowserLocalServiceCloseBrowserViewerProcedure:
		return 20 * time.Second
	default:
		return 0
	}
}
