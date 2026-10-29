package version_test

import (
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

func TestIndependentProcessVersionBoundaries(t *testing.T) {
	lab := newLaboratory(t)
	_, configuration := lab.clientConfiguration(t, "actual-agent")
	lab.start(t, lab.agent, nil, "--config", configuration)
	client := openFrontend(t, configuration)
	for _, test := range []struct {
		name          string
		offer         *pb.ProtocolOffer
		unimplemented bool
		accepted      bool
	}{
		{"compatible-range", &pb.ProtocolOffer{Major: 1, MinMinor: 0, MaxMinor: 4, Capabilities: []string{"terminal.v1", "stats.v1"}}, false, true},
		{"wrong-major", &pb.ProtocolOffer{Major: 2, Capabilities: []string{"terminal.v1"}}, false, false},
		{"disjoint-minor", &pb.ProtocolOffer{Major: 1, MinMinor: 1, MaxMinor: 3, Capabilities: []string{"terminal.v1"}}, false, false},
		{"required-future-capability", &pb.ProtocolOffer{Major: 1, Capabilities: []string{"terminal.v1", "synthetic.future.v1"}, RequiredCapabilities: []string{"synthetic.future.v1"}}, false, false},
		{"missing-negotiation", &pb.ProtocolOffer{Major: 1}, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			authDir := filepath.Join(lab.root, "auth-"+test.name)
			token, id := makeCredential(t, authDir)
			_, fixture := lab.fixture(t, test.name, fixtureConfiguration{Mode: "daemon", Offer: test.offer, Auth: authDir, Build: "synthetic-peer-b", Unimplemented: test.unimplemented})
			server := addRemote(t, client, lab, test.name, fixtureAddress(t, fixture), token, id)
			_, err := client.Remote(server).ListProjects(t.Context(), &pb.ListProjectsRequest{})
			if test.accepted {
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(fixtureCalls(t, fixture), "ListProjects") {
					t.Fatal("compatible peer did not receive ordinary operation")
				}
			} else {
				want := connect.CodeFailedPrecondition
				if test.unimplemented {
					want = connect.CodeUnimplemented
				}
				requireCode(t, err, want)
				if !strings.Contains(fixtureCalls(t, fixture), "NegotiateProtocol") {
					t.Fatal("failure did not exercise authenticated negotiation")
				}
				if strings.Contains(fixtureCalls(t, fixture), "ListProjects") || strings.Contains(fixtureCalls(t, fixture), "GetSystemInfo") {
					t.Fatal("negotiation failure triggered legacy/business fallback")
				}
			}
		})
	}
	t.Run("missing-optional-capability", func(t *testing.T) {
		authDir := filepath.Join(lab.root, "auth-optional")
		token, id := makeCredential(t, authDir)
		_, fixture := lab.fixture(t, "optional", fixtureConfiguration{Mode: "daemon", Offer: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"terminal.v1"}}, Auth: authDir, Build: "synthetic-no-stats"})
		server := addRemote(t, client, lab, "optional", fixtureAddress(t, fixture), token, id)
		_, err := client.Remote(server).GetStats(t.Context(), &pb.StatsRequest{})
		requireCode(t, err, connect.CodeFailedPrecondition)
		if description := version.Describe(err, fixtureBuild); !strings.Contains(description, "stats.v1") || !strings.Contains(description, "daemon") {
			t.Fatal("proxy lost actionable capability diagnostic", description)
		}
		if strings.Contains(fixtureCalls(t, fixture), "GetStats") {
			t.Fatal("agent sent unsupported stats operation instead of gating capability")
		}
	})
}

func TestSameEndpointProcessUpgradeRechecksCapabilities(t *testing.T) {
	lab := newLaboratory(t)
	_, configuration := lab.clientConfiguration(t, "cached-agent")
	lab.start(t, lab.agent, nil, "--config", configuration)
	client := openFrontend(t, configuration)
	authDir := filepath.Join(lab.root, "auth-changing")
	token, id := makeCredential(t, authDir)
	before, path := lab.fixture(t, "before", fixtureConfiguration{Mode: "daemon", Offer: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"terminal.v1", "stats.v1"}}, Auth: authDir, Build: "synthetic-upgrade-a"})
	address := fixtureAddress(t, path)
	server := addRemote(t, client, lab, "changing", address, token, id)
	remote := client.Remote(server)
	if _, err := remote.GetStats(t.Context(), &pb.StatsRequest{}); err != nil {
		t.Fatal(err)
	}
	otherAuth := filepath.Join(lab.root, "auth-independent")
	otherToken, otherID := makeCredential(t, otherAuth)
	_, otherPath := lab.fixture(t, "independent", fixtureConfiguration{Mode: "daemon", Offer: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"stats.v1"}}, Auth: otherAuth, Build: "synthetic-independent"})
	otherServer := addRemote(t, client, lab, "independent", fixtureAddress(t, otherPath), otherToken, otherID)
	before.stop(t)
	_, afterPath := lab.fixture(t, "after", fixtureConfiguration{Mode: "daemon", Address: address, Offer: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"terminal.v1"}}, Auth: authDir, Build: "synthetic-upgrade-b"})
	// Keep the frontend, server record, credential, daemon ID and HTTP client.
	// net/http must reconnect the old pool to the replacement process itself.
	_, err := remote.GetStats(t.Context(), &pb.StatsRequest{})
	requireCode(t, err, connect.CodeFailedPrecondition)
	if strings.Contains(fixtureCalls(t, afterPath), "GetStats") {
		t.Fatal("stale cached capability sent a request to replacement process")
	}
	if !strings.Contains(fixtureCalls(t, afterPath), "NegotiateProtocol") {
		t.Fatal("new process was not re-negotiated")
	}
	if _, err := remote.ListProjects(t.Context(), &pb.ListProjectsRequest{}); err != nil {
		t.Fatal("missing optional capability disabled core methods", err)
	}
	if _, err := client.Remote(otherServer).GetStats(t.Context(), &pb.StatsRequest{}); err != nil {
		t.Fatal("one server's feature change disabled another server", err)
	}
}

func TestInvalidRequiredAgreementCannotPersistSyncMapping(t *testing.T) {
	lab := newLaboratory(t)
	_, configuration := lab.clientConfiguration(t, "mapping-agent")
	lab.start(t, lab.agent, nil, "--config", configuration)
	client := openFrontend(t, configuration)
	authDir := filepath.Join(lab.root, "mapping-auth")
	token, id := makeCredential(t, authDir)
	_, fixture := lab.fixture(t, "mapping-peer", fixtureConfiguration{Mode: "daemon", Offer: &pb.ProtocolOffer{Major: 1, Capabilities: []string{sdk.FileCapability}}, Auth: authDir, Build: "synthetic-bad-agreement", InvalidRequiredAgreement: true})
	server := addRemote(t, client, lab, "mapping-peer", fixtureAddress(t, fixture), token, id)
	_, err := client.Agent().PutMapping(t.Context(), &apb.PutMappingRequest{RequestId: "bad-agreement", Mapping: &ppb.Mapping{ServerId: server, ProjectId: "proj_fixture", LocalRoot: lab.directory(t, "mapping-root"), SyncEnabled: true}})
	requireCode(t, err, connect.CodeFailedPrecondition)
	mappings, err := client.Agent().ListMappings(t.Context(), &apb.ListMappingsRequest{})
	if err != nil || len(mappings.Mappings) != 0 {
		t.Fatalf("invalid agreement persisted mapping: %v %v", mappings, err)
	}
}
