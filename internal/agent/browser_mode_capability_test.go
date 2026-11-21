package agent

import (
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

func TestBrowserStartRequiresModeAwareDaemon(t *testing.T) {
	pool := &remotePool{}
	pool.setCapabilities(&pb.ProtocolAgreement{Capabilities: []string{sdk.BrowserCapability}})
	err := pool.requireCapability(erpc.BrowserServiceStartBrowserProcedure)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(version.Describe(err, "test"), sdk.BrowserLaunchModeCapability) {
		t.Fatalf("old daemon start not rejected: %v", err)
	}
	if err := pool.requireCapability(erpc.BrowserServiceGetBrowserStatusProcedure); err != nil {
		t.Fatal("old daemon status blocked", err)
	}
	pool.setCapabilities(&pb.ProtocolAgreement{Capabilities: []string{sdk.BrowserCapability, sdk.BrowserLaunchModeCapability}})
	if err := pool.requireCapability(erpc.BrowserServiceStartBrowserProcedure); err != nil {
		t.Fatal(err)
	}
	// A worker becoming unavailable withdraws the negotiated capability.
	pool.setCapabilities(&pb.ProtocolAgreement{Capabilities: []string{sdk.BrowserCapability}})
	if err := pool.requireCapability(erpc.BrowserServiceStartBrowserProcedure); err == nil {
		t.Fatal("stale mode capability retained")
	}
}

func TestBrowserLaunchModeDoesNotReplaceBaseRemoteCapability(t *testing.T) {
	pool := &remotePool{}
	pool.setCapabilities(&pb.ProtocolAgreement{Capabilities: []string{sdk.BrowserLaunchModeCapability}})
	err := pool.requireCapability(erpc.BrowserServiceStartBrowserProcedure)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(version.Describe(err, "test"), sdk.BrowserCapability) {
		t.Fatal("base browser service requirement bypassed", err)
	}
}
