package agent_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type runtimeActionEnvelope struct {
	Kind string         `json:"kind"`
	JSON map[string]any `json:"json"`
}

type runtimeAgentProcess struct {
	command *exec.Cmd
	client  *frontend.Client
	output  bytes.Buffer
	done    chan struct{}
	err     error
	once    sync.Once
}

func startRuntimeAgent(t *testing.T, ctx context.Context, binary, configuration string) *runtimeAgentProcess {
	t.Helper()
	process := &runtimeAgentProcess{command: exec.CommandContext(ctx, binary, "--config", configuration), done: make(chan struct{})}
	process.command.Stdout, process.command.Stderr = &process.output, &process.output
	if err := process.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		process.err = process.command.Wait()
		close(process.done)
	}()
	t.Cleanup(func() { process.stop(t) })
	waitRuntime(t, ctx, func(call context.Context) error {
		select {
		case <-process.done:
			t.Fatalf("agent exited during startup: %v\n%s", process.err, process.output.String())
		default:
		}
		var err error
		// The probe context bounds startup; retain production budgets for later lifecycle RPCs.
		process.client, err = frontend.Open(call, configuration, frontend.Options{})
		return err
	})
	return process
}

func (process *runtimeAgentProcess) stop(t *testing.T) {
	t.Helper()
	process.once.Do(func() {
		if process.client != nil {
			process.client.Close()
		}
		select {
		case <-process.done:
		default:
			_ = process.command.Process.Signal(syscall.SIGTERM)
			select {
			case <-process.done:
			case <-time.After(15 * time.Second):
				_ = process.command.Process.Kill()
				<-process.done
				t.Error("agent did not join plugin cleanup before shutdown")
			}
		}
		if process.err != nil {
			t.Errorf("agent shutdown: %v\n%s", process.err, process.output.String())
		}
	})
}

func waitRuntime(t *testing.T, ctx context.Context, check func(context.Context) error) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var last error
	for deadline.Err() == nil {
		call, stop := context.WithTimeout(deadline, time.Second)
		last = check(call)
		stop()
		if last == nil {
			return
		}
		select {
		case <-deadline.Done():
		case <-time.After(30 * time.Millisecond):
		}
	}
	t.Fatal("plugin runtime did not settle:", last)
}

func runtimeBuild(t *testing.T, ctx context.Context, directory, name, source string) string {
	t.Helper()
	binary := filepath.Join(directory, name)
	if output, err := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, source).CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, output)
	}
	return binary
}

func runtimeCopy(t *testing.T, source, destination string) string {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, hash), in)
	closeErr := out.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func runtimeWriteManifest(t *testing.T, directory string, manifest *sdk.PluginManifest, binarySHA256 string) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	receipt, err := json.Marshal(map[string]string{"manifest_sha256": hex.EncodeToString(sum[:]), "binary_sha256": binarySHA256})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".artifact-integrity.json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
}

func runtimePluginStatus(ctx context.Context, client *frontend.Client, id string) (*pb.PluginStatus, error) {
	list, err := client.Agent().ListPlugins(ctx, &apb.PluginTarget{Side: apb.PluginTarget_LOCAL})
	if err != nil {
		return nil, err
	}
	for _, status := range list.Plugins {
		if status.PluginId == id {
			return status, nil
		}
	}
	return nil, nil
}

func runtimeState(t *testing.T, ctx context.Context, client *frontend.Client, id, state string) *pb.PluginStatus {
	t.Helper()
	var result *pb.PluginStatus
	waitRuntime(t, ctx, func(call context.Context) error {
		var err error
		result, err = runtimePluginStatus(call, client, id)
		if err != nil {
			return err
		}
		if result == nil || result.State != state {
			return fmt.Errorf("%s: want %s, got %v", id, state, result)
		}
		return nil
	})
	return result
}

func runtimeManage(t *testing.T, ctx context.Context, client *frontend.Client, status *pb.PluginStatus, action pb.ManagePluginRequest_Action, values map[string]any) (*pb.PluginStatus, *pb.ManagePluginRequest) {
	t.Helper()
	request := &pb.ManagePluginRequest{PluginId: status.PluginId, ExpectedRevision: status.Revision, Action: action, RequestId: rand.Text()}
	if values != nil {
		var err error
		request.Config, err = structpb.NewStruct(values)
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := client.Agent().ManagePlugin(ctx, &apb.ManagePluginRequest{Target: &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}, Request: request})
	if err != nil {
		t.Fatal("manage plugin:", err)
	}
	return result, request
}

func runtimeAction(ctx context.Context, client *frontend.Client, id, action, command string, flags map[string]any) (*sdk.ActionResult, error) {
	values := map[string]any{"plugin_id": id, "side": "local", "action_id": action, "invocation_id": rand.Text()}
	if command != "" {
		values["command_name"] = command
	}
	if flags != nil {
		values["flags"] = flags
	}
	request, err := structpb.NewStruct(values)
	if err != nil {
		return nil, err
	}
	result, err := client.Actions().InvokePluginAction(ctx, request)
	if err != nil {
		return nil, err
	}
	return sdk.UnmarshalActionResult(result)
}

func runtimeEcho(t *testing.T, ctx context.Context, client *frontend.Client, label string) *sdk.ActionResult {
	t.Helper()
	result, err := runtimeAction(ctx, client, "fixture", "", "show", map[string]any{"json": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != sdk.ActionResultJSON || result.JSON["version"] != "1.2.3" || result.JSON["side"] != "client" || result.JSON["label"] != label {
		t.Fatal("wrong third-party action result:", result)
	}
	pid, ok := result.JSON["pid"].(float64)
	if !ok || int(pid) == os.Getpid() || pid <= 0 {
		t.Fatal("action did not execute in a real plugin process:", result.JSON)
	}
	flags, ok := result.JSON["flags"].(map[string]any)
	if !ok || flags["suffix"] != "default" || flags["count"] != "1" || flags["json"] != "true" {
		t.Fatal("manifest flag defaults were not applied:", result.JSON)
	}
	return result
}

func runtimePIDGone(t *testing.T, ctx context.Context, pid int) {
	t.Helper()
	waitRuntime(t, ctx, func(context.Context) error {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("old plugin PID %d remains alive", pid)
	})
}

// Both the host and workers are actual subprocesses. Packages are published only
// after the agent is already live, so this does not accidentally test startup-
// only discovery or an in-process plugin fallback.
func TestThirdPartyRuntimeDiscoveryPersistenceAndFailClosed(t *testing.T) {
	// Keep the process deadline alive through cleanup so SIGTERM can exercise
	// graceful worker shutdown before the context's kill fallback.
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	t.Cleanup(cancel)
	root, err := os.MkdirTemp("/tmp", "shiki-thirdparty-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	binaryDir := filepath.Join(root, "bin")
	pluginsDir := filepath.Join(root, "plugins")
	for _, directory := range []string{binaryDir, pluginsDir} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fixtureBinary := runtimeBuild(t, ctx, binaryDir, "fixture", "../../fixtures/thirdparty")
	agentBinary := runtimeBuild(t, ctx, binaryDir, "shiki-agent", "../../../cmd/shiki-agent")
	cliBinary := runtimeBuild(t, ctx, binaryDir, "shiki", "../../../cmd/shiki")
	syncBinary := runtimeBuild(t, ctx, binaryDir, "sync", "../../../plugins/sync")
	portBinary := runtimeBuild(t, ctx, binaryDir, "port", "../../../plugins/port")
	configuration := filepath.Join(root, "client.yaml")
	cfg := config.DefaultClient()
	cfg.Tailscale.Enabled = false
	cfg.Tailscale.StateDir = filepath.Join(root, "node")
	cfg.Agent.StateDir = filepath.Join(root, "state")
	cfg.Agent.Socket = filepath.Join(root, "agent.sock")
	cfg.PluginsDir = pluginsDir
	if err := config.SaveClient(configuration, cfg); err != nil {
		t.Fatal(err)
	}
	process := startRuntimeAgent(t, ctx, agentBinary, configuration)
	client := process.client
	if status, err := runtimePluginStatus(ctx, client, "fixture"); err != nil || status != nil {
		t.Fatal("undiscovered plugin unexpectedly exists:", status, err)
	}
	packageDir := filepath.Join(pluginsDir, "fixture")
	if err := os.Mkdir(packageDir, 0700); err != nil {
		t.Fatal(err)
	}
	binarySHA256 := runtimeCopy(t, fixtureBinary, filepath.Join(packageDir, "shiki-plugin-fixture"))
	min, max := 1.0, 3.0
	maxItems := 2
	manifest := &sdk.PluginManifest{
		SchemaVersion: "1", ID: "fixture", Version: "1.2.3", DisplayName: "Third-party fixture",
		Capabilities: []string{sdk.CoreCapability, sdk.UIActionCapability}, Sides: []string{"client"}, Binary: "shiki-plugin-fixture",
		BinaryFlags: []string{"--fixture-token=installed", "--side=${side}"},
		ConfigSchema: []sdk.ConfigField{
			{Key: "label", Type: "string", Required: true, EnumValues: []string{"first", "second"}},
			{Key: "limits", Type: "object", Children: []sdk.ConfigField{{Key: "count", Type: "int", Required: true, Constraints: &sdk.FieldConstraints{Min: &min, Max: &max}}}},
			{Key: "routes", Type: "array", Constraints: &sdk.FieldConstraints{MaxLen: &maxItems}, Children: []sdk.ConfigField{{Key: "route", Type: "string", EnumValues: []string{"safe", "local"}}}},
		},
		UI: sdk.UIContribution{CLICommands: []sdk.UICLICommand{{Name: "show", ActionID: "echo", Flags: []sdk.UICLIFlag{{Name: "json", Type: "bool", Default: "false"}, {Name: "suffix", Type: "string", Default: "default"}, {Name: "count", Type: "int", Default: "1"}}}}},
	}
	runtimeWriteManifest(t, packageDir, manifest, binarySHA256)
	// Private staging content is not a second runnable package.
	if err := os.Mkdir(filepath.Join(pluginsDir, ".plugin-txn-fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	runtimeWriteManifest(t, filepath.Join(pluginsDir, ".plugin-txn-fixture"), manifest, binarySHA256)
	status := runtimeState(t, ctx, client, "fixture", "stopped")
	if status.Enabled || status.Revision == "" {
		t.Fatal("new package must have disabled durable intent:", status)
	}
	for _, invalid := range []map[string]any{
		{}, {"label": "unlisted"}, {"label": "first", "unknown": true},
		{"label": "first", "limits": map[string]any{"count": 4}},
		{"label": "first", "limits": map[string]any{}},
		{"label": "first", "routes": []any{"unsafe"}},
		{"label": "first", "routes": []any{"safe", "local", "safe"}},
	} {
		values, _ := structpb.NewStruct(invalid)
		_, err := client.Agent().ManagePlugin(ctx, &apb.ManagePluginRequest{Target: &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}, Request: &pb.ManagePluginRequest{PluginId: "fixture", Action: pb.ManagePluginRequest_SET_CONFIG, Config: values, ExpectedRevision: status.Revision, RequestId: rand.Text()}})
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatal("invalid public schema config accepted:", invalid, err)
		}
		current := runtimeState(t, ctx, client, "fixture", "stopped")
		if current.Revision != status.Revision || current.Enabled {
			t.Fatal("rejected config changed durable intent:", current)
		}
	}
	status, _ = runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_SET_CONFIG, map[string]any{"label": "first", "limits": map[string]any{"count": 2}, "routes": []any{"safe"}})
	accepted, enable := runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_ENABLE, nil)
	status = runtimeState(t, ctx, client, "fixture", "ready")
	if status.BuildVersion != manifest.Version || !status.Enabled {
		t.Fatal("plugin version/ready state is not exact:", status)
	}
	first := runtimeEcho(t, ctx, client, "first")
	if int(first.JSON["pid"].(float64)) == process.command.Process.Pid {
		t.Fatal("agent executed a plugin action in its own process")
	}
	replay, err := client.Agent().ManagePlugin(ctx, &apb.ManagePluginRequest{Target: &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}, Request: enable})
	if err != nil || !proto.Equal(replay, accepted) {
		t.Fatal("enable replay changed its durable result:", replay, err)
	}
	if echo := runtimeEcho(t, ctx, client, "first"); echo.JSON["instance"] != first.JSON["instance"] {
		t.Fatal("deduplicated enable restarted a process")
	}
	if _, err := runtimeAction(ctx, client, "fixture", "", "show", map[string]any{"count": "not-int"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("invalid declared command flag accepted:", err)
	}
	if _, err := runtimeAction(ctx, client, "fixture", "echo", "", nil); err != nil {
		t.Fatal("raw action invocation failed:", err)
	}
	output, err := exec.CommandContext(ctx, cliBinary, "--config", configuration, "plugins", "fixture", "show", "--json", "--flag", "suffix=cli").CombinedOutput()
	var cliResult runtimeActionEnvelope
	if err != nil || json.Unmarshal(output, &cliResult) != nil || cliResult.Kind != "json" || cliResult.JSON["label"] != "first" || cliResult.JSON["version"] != "1.2.3" {
		t.Fatalf("direct third-party CLI action failed: %v\n%s", err, output)
	}
	accepted, restart := runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_RESTART, nil)
	status = runtimeState(t, ctx, client, "fixture", "ready")
	second := runtimeEcho(t, ctx, client, "first")
	if second.JSON["instance"] == first.JSON["instance"] {
		t.Fatal("restart did not replace the worker")
	}
	replay, err = client.Agent().ManagePlugin(ctx, &apb.ManagePluginRequest{Target: &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}, Request: restart})
	if err != nil || !proto.Equal(replay, accepted) {
		t.Fatal("restart replay changed its durable result:", replay, err)
	}
	if echo := runtimeEcho(t, ctx, client, "first"); echo.JSON["instance"] != second.JSON["instance"] {
		t.Fatal("deduplicated restart restarted the process twice")
	}
	status, _ = runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_SET_CONFIG, map[string]any{"label": "second"})
	status = runtimeState(t, ctx, client, "fixture", "ready")
	runtimeEcho(t, ctx, client, "second")
	status, _ = runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_DISABLE, nil)
	status = runtimeState(t, ctx, client, "fixture", "stopped")
	disabledRevision := status.Revision
	process.stop(t)
	process = startRuntimeAgent(t, ctx, agentBinary, configuration)
	client = process.client
	status = runtimeState(t, ctx, client, "fixture", "stopped")
	if status.Enabled || status.Revision != disabledRevision {
		t.Fatal("disabled intent did not survive agent restart:", status)
	}
	status, _ = runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_ENABLE, nil)
	status = runtimeState(t, ctx, client, "fixture", "ready")
	runtimeEcho(t, ctx, client, "second")
	enabledRevision := status.Revision
	process.stop(t)
	process = startRuntimeAgent(t, ctx, agentBinary, configuration)
	client = process.client
	status = runtimeState(t, ctx, client, "fixture", "ready")
	if !status.Enabled || status.Revision != enabledRevision {
		t.Fatal("enabled intent changed during agent recovery:", status)
	}
	beforeRemoval := runtimeEcho(t, ctx, client, "second")
	hiddenPackage := filepath.Join(pluginsDir, ".plugin-txn-removed")
	if err := os.Rename(packageDir, hiddenPackage); err != nil {
		t.Fatal(err)
	}
	status = runtimeState(t, ctx, client, "fixture", "failed")
	if !status.Enabled || status.Revision != enabledRevision || status.ReasonCode != "PLUGIN_ARTIFACT_MISSING" || status.Protocol != nil {
		t.Fatal("removed artifact retained runnable admission or lost intent:", status)
	}
	if _, err := runtimeAction(ctx, client, "fixture", "echo", "", nil); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("removed artifact admitted action:", err)
	}
	runtimePIDGone(t, ctx, int(beforeRemoval.JSON["pid"].(float64)))
	status, _ = runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_DISABLE, nil)
	status = runtimeState(t, ctx, client, "fixture", "stopped")
	disabledRevision = status.Revision
	process.stop(t)
	process = startRuntimeAgent(t, ctx, agentBinary, configuration)
	client = process.client
	status = runtimeState(t, ctx, client, "fixture", "stopped")
	if status.Enabled || status.Revision != disabledRevision {
		t.Fatal("temporarily missing disabled intent was not retained:", status)
	}
	if err := os.Rename(hiddenPackage, packageDir); err != nil {
		t.Fatal(err)
	}
	status = runtimeState(t, ctx, client, "fixture", "stopped")
	if status.Enabled || status.Revision != disabledRevision {
		t.Fatal("package reappearance silently enabled or rewrote intent:", status)
	}
	status, _ = runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_ENABLE, nil)
	status = runtimeState(t, ctx, client, "fixture", "ready")
	beforeCorruption := runtimeEcho(t, ctx, client, "second")
	corrupt := filepath.Join(packageDir, ".corrupt-binary")
	if err := os.WriteFile(corrupt, []byte("not a plugin executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(corrupt, filepath.Join(packageDir, manifest.Binary)); err != nil {
		t.Fatal(err)
	}
	status = runtimeState(t, ctx, client, "fixture", "failed")
	if status.ReasonCode != "PLUGIN_ARTIFACT_INVALID" || status.Protocol != nil {
		t.Fatal("corrupt artifact remained compatible:", status)
	}
	if _, err := runtimeAction(ctx, client, "fixture", "echo", "", nil); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("corrupt package admitted an action:", err)
	}
	runtimePIDGone(t, ctx, int(beforeCorruption.JSON["pid"].(float64)))
	status, _ = runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_DISABLE, nil)
	status = runtimeState(t, ctx, client, "fixture", "stopped")
	runtimeCopy(t, fixtureBinary, filepath.Join(packageDir, manifest.Binary))
	// Launch compatibility is independent of artifact integrity. A trusted but
	// incompatible executable is killed before it can reach ready.
	for _, mode := range []string{"side", "version", "protocol", "capability", "identity"} {
		declaration := *manifest
		switch mode {
		case "side":
			declaration.Sides = []string{"server"}
		case "version":
			declaration.Version = "9.9.9"
		case "protocol":
			declaration.BinaryFlags = append(append([]string(nil), manifest.BinaryFlags...), "--protocol-major=2")
		case "capability":
			declaration.Capabilities = append(append([]string(nil), manifest.Capabilities...), "missing.fixture.v1")
		case "identity":
			declaration.ID, declaration.Binary = "other", "shiki-plugin-other"
		}
		runtimeWriteManifest(t, packageDir, &declaration, binarySHA256)
		request := &pb.ManagePluginRequest{PluginId: "fixture", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: status.Revision, RequestId: rand.Text()}
		result, err := client.Agent().ManagePlugin(ctx, &apb.ManagePluginRequest{Target: &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}, Request: request})
		if mode == "side" || mode == "identity" {
			if connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatal("invalid manifest admitted enable:", mode, result, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(mode, err)
		}
		status = runtimeState(t, ctx, client, "fixture", "incompatible")
		if status.Protocol != nil {
			t.Fatal("incompatible process advertised capabilities:", mode, status)
		}
		status, _ = runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_DISABLE, nil)
		status = runtimeState(t, ctx, client, "fixture", "stopped")
	}
	runtimeWriteManifest(t, packageDir, manifest, binarySHA256)
	// A built-in action must traverse the same actual agent/subprocess route,
	// with ui.action.v1 negotiated only by the supporting client side.
	for _, builtinCase := range []struct {
		id, binary, action, command, field string
	}{
		{"sync", syncBinary, "get-sync-status", "status", "mappings"},
		{"port", portBinary, "list-forwards", "list", "forwards"},
	} {
		id := builtinCase.id
		binarySHA256 := runtimeCopy(t, builtinCase.binary, filepath.Join(pluginsDir, "shiki-plugin-"+id))
		declaration, err := host.ReadPluginManifest(filepath.Join("../../../plugins", id))
		if err != nil {
			t.Fatal(err)
		}
		declaration.Version = "dev"
		offer, _ := sdk.BuiltinProtocol(id, "both")
		legacy, err := json.Marshal(host.Manifest{Name: id, Version: "dev", Side: "both", Executable: "shiki-plugin-" + id, SHA256: binarySHA256, Protocol: offer, PluginManifest: declaration})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pluginsDir, id+".manifest.json"), legacy, 0600); err != nil {
			t.Fatal(err)
		}
		status := runtimeState(t, ctx, client, id, "stopped")
		runtimeManage(t, ctx, client, status, pb.ManagePluginRequest_ENABLE, nil)
		runtimeState(t, ctx, client, id, "ready")
		builtin, err := runtimeAction(ctx, client, id, builtinCase.action, "", map[string]any{"json": "true"})
		if err != nil || builtin.Kind != sdk.ActionResultJSON {
			t.Fatal("built-in JSON action failed:", id, builtin, err)
		}
		if _, ok := builtin.JSON[builtinCase.field].([]any); !ok {
			t.Fatal("built-in JSON action did not return an array:", id, builtin)
		}
		discovery, err := runtimeAction(ctx, client, id, "list-actions", "", nil)
		if err != nil {
			t.Fatal("built-in action discovery failed:", id, err)
		}
		actions, ok := discovery.JSON["actions"].([]any)
		found := false
		for _, action := range actions {
			entry, _ := action.(map[string]any)
			found = found || entry["id"] == builtinCase.action
		}
		if !ok || !found {
			t.Fatal("built-in discovery omitted its implemented action:", id, discovery)
		}
		for _, args := range [][]string{{"plugins", "action", "invoke", id, builtinCase.action, "--json"}, {"plugins", id, builtinCase.command, "--json"}} {
			output, err := exec.CommandContext(ctx, cliBinary, append([]string{"--config", configuration}, args...)...).CombinedOutput()
			var result runtimeActionEnvelope
			if err != nil || json.Unmarshal(output, &result) != nil || result.Kind != "json" {
				t.Fatalf("built-in CLI action failed: %v\n%s", err, output)
			}
			if _, ok := result.JSON[builtinCase.field].([]any); !ok {
				t.Fatalf("built-in CLI returned a text envelope instead of %s: %s", builtinCase.field, output)
			}
		}
	}
}
