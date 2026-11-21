package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

type SystemManager struct{}

const runtimeReadyTimeout = 90 * time.Second

func (SystemManager) Prepare(_ context.Context, state, profileID, profileRevision, instanceID string, region *epb.RegionConfig, revision string, width, height int, mode string) (Spec, error) {
	if mode != ModeBrowse && mode != ModeDiagnostic {
		return Spec{}, errors.New("new browser launches require an explicit browse or diagnostic mode")
	}
	if runtime.GOOS != "linux" || os.Getuid() <= 0 {
		return Spec{}, errors.New("browser requires an unprivileged Linux systemd user")
	}
	if err := verifyExecutable(Binary); err != nil {
		return Spec{}, err
	}
	executableSHA, err := executableDigest(Binary)
	if err != nil {
		return Spec{}, err
	}
	sum := sha256.Sum256([]byte(state))
	namespace := hex.EncodeToString(sum[:6])
	runtimeDir := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	info, err := os.Lstat(runtimeDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || !owned(info) {
		return Spec{}, errors.New("browser requires its private systemd user runtime directory; run administrator browser setup")
	}
	base := filepath.Join(runtimeDir, "shiki-browser-"+namespace)
	if err := PrivateDir(base); err != nil {
		return Spec{}, err
	}
	socketDir := filepath.Join(base, instanceID)
	if err := PrivateDir(socketDir); err != nil {
		return Spec{}, err
	}
	profileDir := filepath.Join(state, "profiles", profileID)
	for _, path := range []string{filepath.Join(state, "profiles"), profileDir, filepath.Join(profileDir, "uploads"), filepath.Join(profileDir, "downloads"), filepath.Join(profileDir, "chrome"), filepath.Join(state, "runtime")} {
		if err := PrivateDir(path); err != nil {
			return Spec{}, err
		}
	}
	spec := Spec{ProfileID: profileID, ProfileRevision: profileRevision, InstanceID: instanceID, Token: NewID(), Unit: "shiki-browser-" + namespace, ConfigPath: filepath.Join(state, "runtime", instanceID+".launch.json"), SocketDir: socketDir, ProfileDir: profileDir, ExecutableSHA256: executableSHA, Region: region, RegionRevision: revision, Width: width, Height: height, Mode: mode}
	if err := spec.Validate(); err != nil {
		return Spec{}, err
	}
	if err := AtomicJSON(spec.ConfigPath, spec, MaxControlBody); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// Plugin hosts intentionally strip ambient session variables. Select only the
// current OS user's fixed systemd bus, never a caller-supplied bus address.
func userServiceCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	run := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	process := exec.CommandContext(ctx, name, args...)
	process.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + os.Getenv("HOME"), "XDG_RUNTIME_DIR=" + run, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + filepath.Join(run, "bus")}
	return process
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := userServiceCommand(ctx, name, args...)
	var output limitedBuffer
	command.Stdout, command.Stderr = &output, &output
	err := command.Run()
	if err != nil {
		return nil, fmt.Errorf("browser service manager: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return output.Bytes(), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	length := len(data)
	if buffer.Len() < MaxControlBody {
		_, _ = buffer.Buffer.Write(data[:min(len(data), MaxControlBody-buffer.Len())])
	}
	return length, nil
}

func (manager SystemManager) Launch(ctx context.Context, spec Spec) (Snapshot, error) {
	if err := spec.Validate(); err != nil {
		return Snapshot{}, err
	}
	lease, err := acquireInstallLease()
	if err != nil {
		return Snapshot{}, err
	}
	defer lease.Close()
	if err := verifyExecutable(Binary); err != nil {
		return Snapshot{}, err
	}
	digest, err := executableDigest(Binary)
	if err != nil {
		return Snapshot{}, err
	}
	if digest != spec.ExecutableSHA256 {
		return Snapshot{}, errors.New("installed runtime changed after this launch was prepared; issue a new explicit start")
	}
	existing := false
	// A failed/stopped prior transient unit may still be collected by systemd.
	// Never restart or replace an unidentified live unit.
	if properties, err := unitProperties(ctx, spec.Unit); err == nil && properties["LoadState"] != "not-found" {
		if err := verifyUnit(properties, spec); err != nil {
			return Snapshot{}, err
		}
		existing = properties["ActiveState"] == "active" || properties["ActiveState"] == "activating"
		if !existing {
			_, _ = command(ctx, "/usr/bin/systemctl", "--user", "reset-failed", spec.Unit+".service")
		}
	}
	if !existing {
		if _, err := command(ctx, "/usr/bin/systemd-run", serviceArguments(spec)...); err != nil {
			return Snapshot{}, err
		}
	}
	snapshot, err := manager.Inspect(ctx, spec)
	if err != nil {
		return snapshot, err
	}
	if snapshot.State != "running" {
		return snapshot, errors.New(snapshot.Reason)
	}
	return snapshot, nil
}

func serviceArguments(spec Spec) []string {
	return []string{"--user", "--collect", "--unit=" + spec.Unit, "--service-type=exec", "--property=Description=ShikigamiBrowser:" + spec.InstanceID, "--property=Restart=no", "--property=KillMode=mixed", "--property=TimeoutStopSec=15", "--property=UMask=0077", "--property=LimitCORE=0", "--", Binary, "--config", spec.ConfigPath}
}

func unitProperties(ctx context.Context, unit string) (map[string]string, error) {
	if !ValidID(unit) {
		return nil, errors.New("invalid browser unit")
	}
	process := userServiceCommand(ctx, "/usr/bin/systemctl", "--user", "show", unit+".service", "--property=LoadState,ActiveState,SubState,Description,Transient,ExecStart,MainPID,Result")
	var data limitedBuffer
	process.Stdout, process.Stderr = &data, &data
	runErr := process.Run()
	values := make(map[string]string)
	for _, line := range strings.Split(data.String(), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	if values["LoadState"] == "not-found" {
		return values, nil
	}
	if runErr != nil {
		return nil, fmt.Errorf("browser unit lookup failed: %w", runErr)
	}
	if values["LoadState"] == "" {
		return nil, errors.New("browser unit manager returned no load state")
	}
	return values, nil
}

func verifyUnit(values map[string]string, spec Spec) error {
	if values["Description"] != "ShikigamiBrowser:"+spec.InstanceID || values["Transient"] != "yes" {
		return errors.New("browser unit identity mismatch; refusing to manage it")
	}
	execStart := values["ExecStart"]
	if !strings.Contains(execStart, "path="+Binary+" ;") || !strings.Contains(execStart, "argv[]="+Binary+" --config "+spec.ConfigPath+" ;") {
		return errors.New("browser unit executable/config identity mismatch")
	}
	return nil
}

func (SystemManager) Inspect(ctx context.Context, spec Spec) (Snapshot, error) {
	return inspectRuntime(ctx, spec, unitProperties)
}

func inspectRuntime(ctx context.Context, spec Spec, readUnit func(context.Context, string) (map[string]string, error)) (Snapshot, error) {
	if err := spec.Validate(); err != nil {
		return Snapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeReadyTimeout)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		properties, err := readUnit(ctx, spec.Unit)
		if err != nil {
			return Snapshot{}, err
		}
		if properties["LoadState"] == "not-found" {
			return exitSnapshot(spec, "runtime unit is absent (no automatic restart)"), nil
		}
		if err := verifyUnit(properties, spec); err != nil {
			return Snapshot{}, err
		}
		if properties["ActiveState"] != "active" && properties["ActiveState"] != "activating" {
			return exitSnapshot(spec, "runtime unit exited: "+properties["Result"]+" (manual restart required)"), nil
		}
		var status Snapshot
		err = controlRequest(ctx, spec, "/status", nil, &status)
		if err == nil {
			if status.ProfileID != spec.ProfileID || status.InstanceID != spec.InstanceID {
				return Snapshot{}, errors.New("runtime socket identity mismatch")
			}
			if status.Mode == "" && spec.Mode == "" {
				status.Mode = ModeDiagnostic
			}
			if status.Mode != spec.BrowserMode() {
				return Snapshot{}, errors.New("runtime browser mode mismatch")
			}
			if status.State != "starting" {
				return status, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			return Snapshot{}, err
		}
		// Only a verified live unit with an absent/refused socket (or its
		// authenticated starting status) may wait. Recheck identity each time;
		// never launch, replace, or adopt a different instance here.
		select {
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		case <-tick.C:
		}
	}
}

func recordRuntimeExit(ctx context.Context, spec Spec, stoppedReason string, failure error) error {
	if ctx.Err() != nil {
		failure = nil
		stoppedReason = "browser runtime stopped by request; manual start required"
	}
	if failure == nil && stoppedReason == "" {
		return nil
	}
	state, reason := "stopped", stoppedReason
	if failure != nil {
		state, reason = "crashed", failure.Error()
	}
	if len(reason) > 4096 {
		reason = reason[:4096]
	}
	snapshot := Snapshot{ProfileID: spec.ProfileID, InstanceID: spec.InstanceID, State: state, Reason: reason, RegionRevision: spec.RegionRevision, Mode: spec.BrowserMode()}
	if err := AtomicJSON(filepath.Join(spec.SocketDir, "exit.json"), snapshot, MaxControlBody); err != nil {
		return errors.Join(failure, fmt.Errorf("persisting private runtime exit reason: %w", err))
	}
	return failure
}

func exitSnapshot(spec Spec, fallback string) Snapshot {
	base := Snapshot{ProfileID: spec.ProfileID, InstanceID: spec.InstanceID, RegionRevision: spec.RegionRevision, State: "crashed", Reason: fallback, Mode: spec.BrowserMode()}
	var snapshot Snapshot
	if ReadJSON(filepath.Join(spec.SocketDir, "exit.json"), &snapshot, MaxControlBody) == nil && snapshot.ProfileID == spec.ProfileID && snapshot.InstanceID == spec.InstanceID && (snapshot.State == "crashed" || snapshot.State == "stopped") && len(snapshot.Reason) <= 4096 {
		mode := snapshot.Mode
		if mode == "" && spec.Mode == "" {
			mode = ModeDiagnostic
		}
		if mode == spec.BrowserMode() {
			base.State, base.Reason = snapshot.State, snapshot.Reason
		}
	}
	return base
}

func (SystemManager) Stop(ctx context.Context, spec Spec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	properties, err := unitProperties(ctx, spec.Unit)
	if err != nil {
		return err
	}
	if properties["LoadState"] == "not-found" {
		return nil
	}
	if err := verifyUnit(properties, spec); err != nil {
		return err
	}
	if _, err := command(ctx, "/usr/bin/systemctl", "--user", "stop", spec.Unit+".service"); err != nil {
		return err
	}
	// Collect failed transient units too; a later manual Start receives a new
	// instance identity and must not inherit an old service's launch record.
	_, _ = command(ctx, "/usr/bin/systemctl", "--user", "reset-failed", spec.Unit+".service")
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		current, err := unitProperties(ctx, spec.Unit)
		if err != nil {
			return err
		}
		if current["LoadState"] == "not-found" {
			return nil
		}
		if err := verifyUnit(current, spec); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("verified browser unit did not stop")
		case <-tick.C:
		}
	}
}

func dialPrivate(ctx context.Context, path string) (net.Conn, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0077 != 0 || !owned(info) {
		return nil, errors.New("browser socket is not private and user-owned")
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	if err := verifyPeer(connection); err != nil {
		connection.Close()
		return nil, err
	}
	return connection, nil
}

func (manager SystemManager) Dial(ctx context.Context, spec Spec) (net.Conn, error) {
	snapshot, err := manager.Inspect(ctx, spec)
	if err != nil {
		return nil, err
	}
	if snapshot.State != "running" {
		return nil, errors.New("browser runtime is not running")
	}
	return dialPrivate(ctx, filepath.Join(spec.SocketDir, "vnc.sock"))
}

func controlRequest(ctx context.Context, spec Spec, path string, body, result any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialPrivate(ctx, filepath.Join(spec.SocketDir, "control.sock"))
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://browser-runtime"+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+spec.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("private browser runtime operation failed (%d)", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	return decoder.Decode(result)
}

func (manager SystemManager) Diagnose(ctx context.Context, spec Spec, request *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.BrowserMode() == ModeBrowse {
		return nil, errors.New(DiagnosticsDisabledReason)
	}
	if _, err := manager.Inspect(ctx, spec); err != nil {
		return nil, err
	}
	var report epb.DiagnosticReport
	if err := controlRequest(ctx, spec, "/diagnose", request, &report); err != nil {
		return nil, err
	}
	appendLaunchConfiguration(&report, spec)
	return &report, nil
}
