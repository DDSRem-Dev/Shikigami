// thirdparty is a real public-SDK plugin used only by cross-process tests.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

var (
	buildVersion = "1.2.3"
	pluginID     = "fixture"
)

type fixture struct {
	side, token, label, instance string
	major                        uint32
}

func (p *fixture) Info() *ppb.PluginInfo {
	return &ppb.PluginInfo{Name: pluginID, Version: buildVersion, Side: p.side, Protocol: &pb.ProtocolOffer{Major: p.major, Capabilities: []string{sdk.CoreCapability, sdk.UIActionCapability}}}
}

func (p *fixture) Init(ctx context.Context, in sdk.InitContext) error {
	if p.token != "installed" || in.Side != p.side || in.Core == nil {
		return connect.NewError(connect.CodeInvalidArgument, "FIXTURE_INIT_CONTRACT")
	}
	// The actual callback verifies own-config scope, instance credentials and
	// callback listener readiness, rather than relying on an in-process stub.
	configuration, err := in.Core.GetConfig(ctx, &ppb.ConfigRequest{Scope: &ppb.Scope{}})
	if err != nil {
		return err
	}
	p.label = configuration.GetValues().GetFields()["label"].GetStringValue()
	p.instance = in.InstanceID
	return nil
}

func (*fixture) Start(context.Context) error { return nil }
func (*fixture) Stop(context.Context) error  { return nil }
func (*fixture) Health(context.Context) (*pb.HealthStatus, error) {
	return &pb.HealthStatus{Status: pb.HealthStatus_HEALTHY}, nil
}

func (p *fixture) InvokeAction(_ context.Context, request *sdk.ActionRequest) (*sdk.ActionResult, error) {
	if request.ActionID != "echo" {
		return nil, connect.NewError(connect.CodeNotFound, "FIXTURE_ACTION_NOT_FOUND")
	}
	flags := make(map[string]any, len(request.Flags))
	for key, value := range request.Flags {
		flags[key] = value
	}
	return &sdk.ActionResult{Kind: sdk.ActionResultJSON, Done: true, JSON: map[string]any{
		"version": buildVersion, "side": p.side, "pid": os.Getpid(), "label": p.label, "instance": p.instance, "flags": flags,
	}}, nil
}

func main() {
	side := flag.String("side", "", "Plugin side")
	token := flag.String("fixture-token", "", "Required fixture startup flag")
	major := flag.Uint("protocol-major", 1, "Advertised protocol major")
	flag.Parse()
	if err := sdk.Serve(*side, &fixture{side: *side, token: *token, major: uint32(*major)}, sdk.Services{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
