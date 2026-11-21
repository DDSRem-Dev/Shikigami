package client

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
)

func TestBrowserStartRequiresModeAwareAgentBeforeSending(t *testing.T) {
	for _, modeAware := range []bool{false, true} {
		capabilities := []string{"browser.remote.v1"}
		if modeAware {
			capabilities = append(capabilities, "browser.launch-mode.v1")
		}
		c := &Client{lifetime: context.Background(), timeout: time.Second, capabilities: capabilities}
		called := false
		call := c.extensionIdentity("server", "browser.remote.v1")(func(context.Context, connect.Spec) (connect.ClientStream, error) {
			called = true
			return nil, context.Canceled
		})
		_, err := call(t.Context(), connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: erpc.BrowserServiceStartBrowserProcedure})
		if called != modeAware {
			t.Fatalf("old agent received mode-sensitive start: called=%v modeAware=%v", called, modeAware)
		}
		if !modeAware && (connect.CodeOf(err) != connect.CodeUnimplemented || !strings.Contains(err.Error(), "browser.launch-mode.v1")) {
			t.Fatalf("missing actionable mode gate: %v", err)
		}
		called = false
		_, _ = call(t.Context(), connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: erpc.BrowserServiceGetBrowserStatusProcedure})
		if !called {
			t.Fatal("old agent lost compatible status query")
		}
	}
}

func TestExtensionCapabilityGateDoesNotRequireNewCapabilitiesForOldClients(t *testing.T) {
	c := &Client{lifetime: context.Background(), timeout: time.Second, capabilities: []string{"agent.control.v1"}}
	for _, capability := range []string{"region.host.v1", "browser.remote.v1", "browser.viewer.v1"} {
		called := false
		call := c.extensionIdentity("server", capability)(func(context.Context, connect.Spec) (connect.ClientStream, error) {
			called = true
			return nil, context.Canceled
		})
		_, err := call(t.Context(), connect.Spec{StreamType: connect.StreamTypeUnary})
		if called || connect.CodeOf(err) != connect.CodeUnimplemented || !strings.Contains(err.Error(), capability) {
			t.Fatalf("missing %s: called=%t err=%v", capability, called, err)
		}
	}
	called := false
	old := c.identity("")(func(context.Context, connect.Spec) (connect.ClientStream, error) {
		called = true
		return nil, context.Canceled
	})
	_, _ = old(t.Context(), connect.Spec{StreamType: connect.StreamTypeUnary})
	if !called {
		t.Fatal("an older business method was gated on extension capabilities")
	}
}

func TestExtensionClientsPreserveIPCIdentityAndLocalRouting(t *testing.T) {
	for _, tc := range []struct {
		capability, server string
	}{
		{"region.host.v1", "server_region"},
		{"browser.remote.v1", "server_browser"},
		{"browser.viewer.v1", ""},
	} {
		t.Run(tc.capability, func(t *testing.T) {
			c := &Client{instance: "instance", frontend: "frontend", lifetime: context.Background(), timeout: time.Second, capabilities: []string{tc.capability}}
			caller, callerInfo := connect.NewClientContext(t.Context())
			callerInfo.RequestHeader().Set("Authorization", "Bearer never-forward-this")
			callerInfo.RequestHeader().Set("X-Secret", "never-forward-this")
			callerInfo.RequestHeader().Set(instanceHeader, "forged-instance")
			callerInfo.RequestHeader().Set(frontendHeader, "forged-frontend")
			callerInfo.RequestHeader().Set(serverHeader, "forged-server")
			called := false
			call := c.extensionIdentity(tc.server, tc.capability)(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
				called = true
				info, ok := connect.CallInfoForClientContext(ctx)
				if !ok {
					t.Fatal("missing private client context")
				}
				h := info.RequestHeader()
				if h.Get(instanceHeader) != "instance" || h.Get(frontendHeader) != "frontend" || h.Get(serverHeader) != tc.server || h.Get("Authorization") != "" || h.Get("X-Secret") != "" {
					t.Fatalf("incorrect extension identity: %v", h)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unary extension call has no bounded deadline")
				}
				return nil, context.Canceled
			})
			_, _ = call(caller, connect.Spec{StreamType: connect.StreamTypeUnary})
			if !called || callerInfo.RequestHeader().Get(serverHeader) != "forged-server" {
				t.Fatal("extension skipped transport or modified the caller's context")
			}
		})
	}
}

func TestRemoteExtensionsRequireResolvedServerIdentity(t *testing.T) {
	c := &Client{lifetime: context.Background(), timeout: time.Second, capabilities: []string{"region.host.v1", "browser.remote.v1"}}
	for _, capability := range c.capabilities {
		called := false
		call := c.extensionIdentity("", capability)(func(context.Context, connect.Spec) (connect.ClientStream, error) {
			called = true
			return nil, context.Canceled
		})
		_, err := call(t.Context(), connect.Spec{StreamType: connect.StreamTypeUnary})
		if called || connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("unscoped remote extension accepted: called=%t err=%v", called, err)
		}
	}
}

func TestExtensionDeadlinesMatchBoundedServerWork(t *testing.T) {
	for _, tc := range []struct {
		procedure        string
		configured, want time.Duration
	}{
		{erpc.RegionServiceInspectRegionProcedure, 5 * time.Second, 20 * time.Second},
		{erpc.RegionServicePlanRegionProcedure, 5 * time.Second, 20 * time.Second},
		{erpc.RegionServiceApplyRegionProcedure, 5 * time.Second, 90 * time.Second},
		{erpc.RegionServiceRollbackRegionProcedure, 5 * time.Second, 90 * time.Second},
		{erpc.RegionServiceDiagnoseRegionProcedure, 5 * time.Second, 40 * time.Second},
		{erpc.BrowserServiceStartBrowserProcedure, 5 * time.Second, 120 * time.Second},
		{erpc.BrowserServiceStopBrowserProcedure, 5 * time.Second, 120 * time.Second},
		{erpc.BrowserServiceDeleteBrowserProfileProcedure, 5 * time.Second, 120 * time.Second},
		{erpc.BrowserServiceDiagnoseBrowserProcedure, 5 * time.Second, 90 * time.Second},
		{erpc.BrowserLocalServiceOpenBrowserViewerProcedure, 5 * time.Second, 20 * time.Second},
		{erpc.RegionServiceApplyRegionProcedure, 2 * time.Second, 2 * time.Second},
	} {
		c := &Client{lifetime: context.Background(), timeout: tc.configured}
		call := c.identity("server")(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
			deadline, ok := ctx.Deadline()
			remaining := time.Until(deadline)
			if !ok || remaining > tc.want || remaining < tc.want-time.Second {
				t.Fatalf("%s timeout = %s, want %s", tc.procedure, remaining, tc.want)
			}
			return nil, context.Canceled
		})
		_, _ = call(t.Context(), connect.Spec{Procedure: tc.procedure, StreamType: connect.StreamTypeUnary})
	}
	c := &Client{lifetime: context.Background(), timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	call := c.identity("server")(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) > 100*time.Millisecond {
			t.Fatal("extension timeout extended the caller deadline")
		}
		return nil, context.Canceled
	})
	_, _ = call(ctx, connect.Spec{Procedure: erpc.RegionServiceApplyRegionProcedure, StreamType: connect.StreamTypeUnary})
}
