package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/proto"
)

type setupFixture struct {
	runs             []string
	queries          []string
	existing         bool
	profiles         []*epb.BrowserProfile
	pluginsReady     bool
	failedCommand    string
	healthErr        error
	healthChecked    bool
	node             *apb.NodeStatus
	nodeSequence     []*apb.NodeStatus
	loginErr         error
	config           map[string]any
	temporaryFile    string
	privateTemporary bool
	renewedToken     bool
	authRequired     bool
	authReason       string
	savedAddress     string
	connectedAddress string
}

func (f *setupFixture) connect(ctx context.Context, server string, renew bool, address string) error {
	f.renewedToken = renew
	f.connectedAddress = address
	return f.run(ctx, true, "connect", server)
}

func (f *setupFixture) run(_ context.Context, _ bool, args ...string) error {
	command := strings.Join(args, " ")
	f.runs = append(f.runs, command)
	if command == "node login" && f.loginErr != nil {
		return f.loginErr
	}
	if f.failedCommand != "" && strings.Contains(command, f.failedCommand) {
		return errors.New("fixture dependency failure")
	}
	if strings.Contains(command, " enable ") {
		f.pluginsReady = true
	}
	for index, arg := range args {
		if arg != "--file" {
			continue
		}
		f.temporaryFile = args[index+1]
		info, err := os.Stat(f.temporaryFile)
		if err != nil {
			return err
		}
		f.privateTemporary = info.Mode().Perm() == 0600
		data, err := os.ReadFile(f.temporaryFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &f.config); err != nil {
			return err
		}
	}
	return nil
}

func (f *setupFixture) query(ctx context.Context, output proto.Message, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.queries = append(f.queries, strings.Join(args, " "))
	switch value := output.(type) {
	case *apb.NodeStatus:
		if len(f.nodeSequence) != 0 {
			f.node = f.nodeSequence[0]
			f.nodeSequence = f.nodeSequence[1:]
		}
		if f.node == nil {
			value.State = "online"
		} else {
			proto.Reset(value)
			proto.Merge(value, f.node)
		}
	case *apb.ServerList:
		if f.existing {
			value.Servers = []*apb.ServerRecord{{Config: &apb.ServerConfig{Id: "server-id", Name: "my-vps", TailscaleAddress: f.savedAddress}, CredentialPresent: true}}
			if f.authRequired {
				value.Servers[0].ConnectionState = "auth_required"
				value.Servers[0].ReasonCode = f.authReason
			}
		}
	case *pb.PluginStatus:
		value.Revision = "observed-revision"
		value.Enabled = f.pluginsReady
		value.State = "stopped"
		if f.pluginsReady {
			value.State = "ready"
		}
	case *epb.RegionStatus:
		value.HelperAvailable = true
		value.PendingOperationId = "existing-dns-operation"
	case *epb.BrowserProfileList:
		value.Profiles = f.profiles
	case *epb.BrowserStatus:
		value.State = "stopped"
	default:
		return errors.New("unexpected fixture query")
	}
	return nil
}

func (f *setupFixture) health(context.Context, string) error {
	f.healthChecked = true
	return f.healthErr
}

func setupTestWizard(t *testing.T, backend *setupFixture, answers ...string) (*setupWizard, *bytes.Buffer) {
	t.Helper()
	var output bytes.Buffer
	wizard := &setupWizard{backend: backend, out: &output}
	wizard.prompt = func(_ context.Context, label, fallback string) (string, error) {
		if len(answers) == 0 {
			t.Fatalf("unexpected prompt: %s (default %s)", label, fallback)
		}
		answer := answers[0]
		answers = answers[1:]
		return answer, nil
	}
	return wizard, &output
}

func TestSetupExistingServerPreservesPluginConfigurationAndProfiles(t *testing.T) {
	f := &setupFixture{existing: true, pluginsReady: true, profiles: []*epb.BrowserProfile{{Id: "user-work", BootStart: true}}}
	wizard, output := setupTestWizard(t, f, "", "")
	if err := wizard.setup(t.Context(), "all", "my-vps"); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range f.runs {
		for _, prohibited := range []string{" config ", " enable ", "profile create", "browser start", "browser open", "region apply", "token"} {
			if strings.Contains(mutation, prohibited) {
				t.Fatalf("repeat setup mutated existing settings: %s", mutation)
			}
		}
	}
	if !f.healthChecked || !strings.Contains(output.String(), "Existing port configuration is preserved") || !strings.Contains(output.String(), "selected plugins verified") {
		t.Fatal("repeat setup did not verify or accurately report preserved state", output.String())
	}
}

func TestSetupNewServerConfiguresOnlyConfirmedPortsAndStartsNewProfile(t *testing.T) {
	f := &setupFixture{}
	wizard, output := setupTestWizard(t, f, "3000,8080", "yes", "yes", "inherit")
	if err := wizard.setup(t.Context(), "all", "my-vps"); err != nil {
		t.Fatal(err)
	}
	runs := strings.Join(f.runs, "\n")
	for _, required := range []string{
		"enable sync --expected-revision observed-revision",
		"config port --side remote --expected-revision observed-revision",
		"browser profile create work --name Work --yes",
		"browser start work --yes",
		"browser open work --read-only=false --launch",
	} {
		if !strings.Contains(runs, required) {
			t.Fatalf("missing confirmed operation %s in %s", required, runs)
		}
	}
	if !f.privateTemporary || !reflect.DeepEqual(f.config["allow_ports"], []any{float64(3000), float64(8080)}) {
		t.Fatal("configuration file lost chosen ports or private permissions", f.config)
	}
	if _, err := os.Stat(f.temporaryFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temporary configuration was retained", err)
	}
	if !f.healthChecked || !strings.Contains(output.String(), "Take control") {
		t.Fatal("final readiness or viewer guidance missing")
	}
}

func TestSetupDecliningPortReplacementDoesNotWriteConfig(t *testing.T) {
	f := &setupFixture{pluginsReady: true}
	wizard, output := setupTestWizard(t, f, "3000", "no")
	if err := wizard.setup(t.Context(), "core", "my-vps"); err != nil {
		t.Fatal(err)
	}
	if f.temporaryFile != "" || strings.Contains(strings.Join(f.runs, "\n"), "config port") || !strings.Contains(output.String(), "no new forward configured") {
		t.Fatal("declined replacement changed port settings", f.runs, output.String())
	}
}

func TestSetupPluginFailureStopsBeforeClaimingReady(t *testing.T) {
	f := &setupFixture{failedCommand: "enable sync"}
	wizard, output := setupTestWizard(t, f)
	if err := wizard.setup(t.Context(), "all", "my-vps"); err == nil {
		t.Fatal("dependency failure was treated as success")
	}
	if f.healthChecked || strings.Contains(output.String(), "selected plugins verified") {
		t.Fatal("setup claimed completion after plugin failure")
	}
}

func TestSetupUnhealthyRemoteDoesNotClaimCompletion(t *testing.T) {
	f := &setupFixture{existing: true, pluginsReady: true, healthErr: errors.New("Claude authorization pending")}
	wizard, output := setupTestWizard(t, f, "")
	err := wizard.setup(t.Context(), "core", "my-vps")
	if err == nil || !strings.Contains(err.Error(), "readiness remains pending") || strings.Contains(output.String(), "selected plugins verified") {
		t.Fatal("unhealthy remote was accepted", err, output.String())
	}
}

func TestSetupNodeWaitIsCancellableAndDoesNotContinueConfiguration(t *testing.T) {
	f := &setupFixture{node: &apb.NodeStatus{State: "authenticating", LoginId: "login", LoginExpiresAt: time.Now().Add(time.Minute).Unix()}}
	wizard, output := setupTestWizard(t, f)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	err := wizard.setup(ctx, "all", "my-vps")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("login wait did not honor cancellation", err)
	}
	if len(f.runs) != 1 || f.runs[0] != "agent start" || f.healthChecked || strings.Contains(output.String(), "selected plugins verified") {
		t.Fatal("cancelled login proceeded to configure server", f.runs)
	}
}

func TestSetupContinuesInstallerLoginWithoutStartingAnotherAttempt(t *testing.T) {
	url := "https://login.example.invalid/installer-attempt"
	f := &setupFixture{nodeSequence: []*apb.NodeStatus{
		{State: "needs_login", ReasonCode: "NODE_AUTH_REQUIRED", LoginId: "installer-login", LoginExpiresAt: time.Now().Add(time.Minute).Unix(), LoginUrl: url},
		{State: "online"},
	}}
	wizard, output := setupTestWizard(t, f)
	if err := wizard.node(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.runs) != 0 || strings.Count(output.String(), url) != 1 {
		t.Fatal("active installer login was restarted or its private URL was not handed off", f.runs, output.String())
	}
}

func TestSetupRecoversConcurrentLoginAlreadyExists(t *testing.T) {
	url := "https://login.example.invalid/concurrent-attempt"
	f := &setupFixture{
		loginErr: connect.NewError(connect.CodeAlreadyExists, "LOGIN_IN_PROGRESS"),
		nodeSequence: []*apb.NodeStatus{
			{State: "needs_login", ReasonCode: "NODE_AUTH_REQUIRED"},
			{State: "authenticating", LoginId: "concurrent-login", LoginExpiresAt: time.Now().Add(time.Minute).Unix(), LoginUrl: url},
			{State: "online"},
		},
	}
	wizard, output := setupTestWizard(t, f)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := wizard.node(ctx); err != nil {
		t.Fatal("concurrent authorization should be observed instead of rejected", err)
	}
	if !reflect.DeepEqual(f.runs, []string{"node login"}) || strings.Count(output.String(), url) != 1 || len(f.queries) != 3 {
		t.Fatal("race recovery restarted login or failed to re-read its state", f.runs, f.queries, output.String())
	}
}

func TestSetupPortInputRejectsInvalidOrUnboundedValues(t *testing.T) {
	for _, value := range []string{"0", "65536", "3000,", "$(touch /tmp/bad)", strings.Repeat("1,", 65)} {
		if _, err := setupPorts(value); err == nil {
			t.Fatal("accepted invalid port list", value)
		}
	}
	ports, err := setupPorts("3000, 8080,3000")
	if err != nil || !reflect.DeepEqual(ports, []int{3000, 8080}) {
		t.Fatal("valid port list rejected", ports, err)
	}
}

func TestSetupCanResumeAnExplicitlySelectedSavedBrowser(t *testing.T) {
	f := &setupFixture{profiles: []*epb.BrowserProfile{{Id: "work"}}}
	wizard, _ := setupTestWizard(t, f, "work", "yes")
	if err := wizard.browser(t.Context(), "my-vps"); err != nil {
		t.Fatal(err)
	}
	runs := strings.Join(f.runs, "\n")
	if !strings.Contains(runs, "browser start work --yes") || !strings.Contains(runs, "browser open work") || strings.Contains(runs, "profile create") || strings.Contains(runs, "replace-running") {
		t.Fatal("explicit recovery replaced profile data or another running browser", runs)
	}
}

func TestSetupCanRenewRejectedCredentialWithoutReconfiguringExistingServer(t *testing.T) {
	f := &setupFixture{existing: true, authRequired: true, pluginsReady: true}
	wizard, _ := setupTestWizard(t, f, "yes", "")
	if err := wizard.setup(t.Context(), "core", "my-vps"); err != nil {
		t.Fatal(err)
	}
	if !f.renewedToken || f.temporaryFile != "" {
		t.Fatal("credential renewal failed or replaced plugin configuration")
	}
}

func TestSetupCorrectsInvalidSavedAddressWithoutRenewingToken(t *testing.T) {
	f := &setupFixture{existing: true, pluginsReady: true, savedAddress: "9527:9527"}
	wizard, output := setupTestWizard(t, f, "9527", "100.71.151.36", "")
	if err := wizard.setup(t.Context(), "core", "my-vps"); err != nil {
		t.Fatal(err)
	}
	if f.connectedAddress != "100.71.151.36:9527" || f.renewedToken {
		t.Fatal("address repair replaced credential or used invalid input", f.connectedAddress, f.renewedToken)
	}
	if !strings.Contains(output.String(), "Saved VPS address: 9527:9527") || !strings.Contains(output.String(), "not a port number") {
		t.Fatal("invalid saved endpoint not explained", output.String())
	}
}

func TestSetupExplicitAddressCorrectionPreservesOtherChoices(t *testing.T) {
	f := &setupFixture{existing: true, pluginsReady: true, savedAddress: "old-host:9527"}
	wizard, _ := setupTestWizard(t, f, "")
	wizard.address = "new-host:9527"
	if err := wizard.setup(t.Context(), "core", "my-vps"); err != nil {
		t.Fatal(err)
	}
	if f.connectedAddress != "new-host:9527" || f.renewedToken {
		t.Fatal("explicit address correction not passed independently", f.connectedAddress, f.renewedToken)
	}
	if f.temporaryFile != "" {
		t.Fatal("address correction changed plugin config")
	}
}

func TestSetupConfirmationRepromptsAndAcceptsShortAnswers(t *testing.T) {
	for _, test := range []struct {
		answer string
		want   bool
	}{{"y", true}, {"YES", true}, {"n", false}, {"No", false}} {
		t.Run(test.answer, func(t *testing.T) {
			f := &setupFixture{pluginsReady: true}
			wizard, output := setupTestWizard(t, f, "3000", "mistyped", test.answer)
			if err := wizard.setup(t.Context(), "core", "my-vps"); err != nil {
				t.Fatal(err)
			}
			if (f.temporaryFile != "") != test.want {
				t.Fatal("unexpected port config mutation", f.runs)
			}
			if !strings.Contains(output.String(), "Enter yes (y) or no (n)") {
				t.Fatal("invalid answer silently consumed", output.String())
			}
		})
	}
}

func TestSetupControlOutageDoesNotStartAnotherAuthorization(t *testing.T) {
	f := &setupFixture{existing: true, pluginsReady: true, node: &apb.NodeStatus{State: "offline", ReasonCode: "NODE_CONTROL_UNAVAILABLE"}}
	wizard, output := setupTestWizard(t, f, "")
	if err := wizard.setup(t.Context(), "core", "my-vps"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(f.runs, "\n"), "node login") || f.renewedToken {
		t.Fatal("control outage triggered authorization", f.runs)
	}
	if !strings.Contains(output.String(), "no new device authorization") {
		t.Fatal("control outage not explained", output.String())
	}
}

func TestSetupNodeAuthorizationDoesNotRenewApplicationToken(t *testing.T) {
	f := &setupFixture{existing: true, authRequired: true, authReason: "NODE_AUTH_REQUIRED", pluginsReady: true}
	wizard, _ := setupTestWizard(t, f, "")
	if err := wizard.setup(t.Context(), "core", "my-vps"); err != nil {
		t.Fatal(err)
	}
	if f.renewedToken {
		t.Fatal("node authorization replaced the saved application Token")
	}
}
