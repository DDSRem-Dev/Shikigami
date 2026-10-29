package version_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
)

func TestRealAgentRejectsIncompatibleFrontendOffers(t *testing.T) {
	lab := newLaboratory(t)
	cfg, path := lab.clientConfiguration(t, "agent-offers")
	lab.start(t, lab.agent, nil, "--config", path)
	existing := openFrontend(t, path)
	endpoint, err := connection.NewUnixClient(cfg.Agent.Socket, uint32(os.Getuid()), "version-test")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	api := arpc.NewAgentClient(connection.NewRPCClient(endpoint))
	for name, offer := range map[string]*pb.ProtocolOffer{
		"major":            {Major: 2, Capabilities: []string{"agent.control.v1"}},
		"minor":            {Major: 1, MinMinor: 1, MaxMinor: 3, Capabilities: []string{"agent.control.v1"}},
		"missing-required": {Major: 1, Capabilities: []string{"agent.control.v1", "future.gui.v1"}, RequiredCapabilities: []string{"future.gui.v1"}},
	} {
		t.Run(name, func(t *testing.T) {
			hello, err := api.Handshake(t.Context(), &apb.HandshakeRequest{Protocol: offer, ClientBuild: "synthetic-frontend-b"})
			requireCode(t, err, connect.CodeFailedPrecondition)
			if hello != nil && hello.FrontendId != "" {
				t.Fatal("failed negotiation issued a frontend identity")
			}
			_, err = api.GetStatus(t.Context(), &pb.Empty{})
			requireCode(t, err, connect.CodeFailedPrecondition)
			if _, err := existing.Agent().GetStatus(t.Context(), &pb.Empty{}); err != nil {
				t.Fatal("rejected peer disrupted existing frontend", err)
			}
		})
	}
	hello, err := api.Handshake(t.Context(), &apb.HandshakeRequest{Protocol: &pb.ProtocolOffer{Major: 1, MinMinor: 0, MaxMinor: 5, Capabilities: []string{"agent.control.v1", "agent.events.v1", "future.gui.v1"}, RequiredCapabilities: []string{"agent.control.v1"}}, ClientBuild: "synthetic-compatible-frontend"})
	if err != nil {
		t.Fatal(err)
	}
	if hello.Protocol.Minor != 0 || hello.Protocol.BuildVersion != fixtureBuild || len(hello.Protocol.Capabilities) != 2 {
		t.Fatalf("incorrect production intersection: %v", hello.Protocol)
	}
}

func TestActualCLIDoesNotReplaceIncompatibleAgentProcess(t *testing.T) {
	lab := newLaboratory(t)
	for _, test := range []struct {
		name    string
		offer   *pb.ProtocolOffer
		invalid bool
		ok      bool
	}{
		{"major", &pb.ProtocolOffer{Major: 2, Capabilities: []string{"agent.control.v1"}}, false, false},
		{"minor", &pb.ProtocolOffer{Major: 1, MinMinor: 1, MaxMinor: 2, Capabilities: []string{"agent.control.v1"}}, false, false},
		{"missing-control", &pb.ProtocolOffer{Major: 1, Capabilities: []string{"agent.events.v1"}}, false, false},
		{"invalid-agreement", &pb.ProtocolOffer{Major: 1, Capabilities: []string{"agent.control.v1"}}, true, false},
		{"compatible", &pb.ProtocolOffer{Major: 1, MaxMinor: 5, Capabilities: []string{"agent.control.v1", "agent.events.v1"}}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, path := lab.clientConfiguration(t, "config-"+test.name)
			child, fixture := lab.fixture(t, "peer-"+test.name, fixtureConfiguration{Mode: "agent", Address: cfg.Agent.Socket, Offer: test.offer, Build: "synthetic-agent-b", InvalidAgreement: test.invalid})
			command := exec.CommandContext(t.Context(), lab.cli, "--config", path, "--json", "agent", "start")
			out, err := command.CombinedOutput()
			if test.ok {
				if err != nil {
					t.Fatalf("compatible CLI failed: %v %s", err, out)
				}
			} else {
				if err == nil {
					t.Fatalf("CLI accepted incompatible peer: %s", out)
				}
				if !test.invalid {
					for _, expected := range []string{"agent", "synthetic-agent-b", fixtureBuild, "major", "minor", "install compatible"} {
						if !strings.Contains(string(out), expected) {
							t.Fatalf("missing safe diagnostic %q: %s", expected, out)
						}
					}
					if test.name == "missing-control" && !strings.Contains(string(out), "agent.control.v1") {
						t.Fatalf("missing capability diagnostic: %s", out)
					}
				}
				if strings.Contains(fixtureCalls(t, fixture), "GetStatus") {
					t.Fatal("CLI continued business calls after failed negotiation")
				}
			}
			if strings.Contains(fixtureCalls(t, fixture), "Shutdown") {
				t.Fatal("CLI stopped an incompatible shared agent")
			}
			select {
			case <-child.done:
				t.Fatal("CLI terminated peer process")
			default:
			}
		})
	}
}
