package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Native struct {
	startLock       *os.File
	Prefix          string
	AuthKey         []byte
	Interactive     bool
	TokenOutput     string
	LoginURL        string
	IssueToken      bool
	TokenTTY        io.Writer
	Report          *ProbeReport
	TokenIssued     bool
	ExpectedVersion string
}

func accountFor(plan Plan) (*user.User, int, int, error) {
	account, err := user.Lookup(plan.User)
	if err != nil {
		return nil, 0, 0, err
	}
	uid, e1 := strconv.Atoi(account.Uid)
	gid, e2 := strconv.Atoi(account.Gid)
	if e1 != nil || e2 != nil || uid == 0 || account.HomeDir != plan.Home {
		return nil, 0, 0, errors.New("an existing ordinary user with matching home is required")
	}
	if os.Geteuid() != 0 && os.Geteuid() != uid {
		return nil, 0, 0, errors.New("installer cannot act as another user")
	}
	return account, uid, gid, nil
}
func command(ctx context.Context, plan Plan, asUser bool, binary string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = time.Second
	cmd.Dir = plan.Home
	cmd.Env = []string{"HOME=" + plan.Home, "PATH=" + plan.Path, "LANG=C.UTF-8"}
	_, uid, gid, err := accountFor(plan)
	if err != nil {
		cmd.Path = "" // Never fall back to privileged execution.
		return cmd
	}
	if err == nil {
		cmd.Env = append(cmd.Env, "XDG_RUNTIME_DIR=/run/user/"+strconv.Itoa(uid))
		if asUser && os.Geteuid() == 0 {
			cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
		}
	}
	return cmd
}
func run(ctx context.Context, plan Plan, asUser bool, binary string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := command(ctx, plan, asUser, binary, args...)
	captured := &cappedOutput{limit: 64 << 10}
	cmd.Stdout = captured
	cmd.Stderr = captured
	err := cmd.Run()
	output, truncated := captured.Bytes()
	if truncated && err == nil {
		err = errors.New("child output exceeds 64 KiB limit")
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w (%s)", filepath.Base(binary), err, strings.TrimSpace(string(output[:min(len(output), 2048)])))
	}
	return nil
}
func (n *Native) userCall(ctx context.Context, plan Plan, operation string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	request := UserOperation{Prefix: n.Prefix, Plan: plan, ExpectedVersion: n.ExpectedVersion}
	if operation == "probe" {
		request.Key = n.AuthKey
		request.Interactive = n.Interactive
	}
	input, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	defer clear(input)
	cmd := command(ctx, plan, true, filepath.Join(n.Prefix, "current", "shiki-install"), "_user-"+operation)
	cmd.Stdin = bytes.NewReader(input)
	captured := &cappedOutput{limit: 64 << 10}
	cmd.Stdout = captured
	cmd.Stderr = io.Discard
	err = cmd.Run()
	output, truncated := captured.Bytes()
	if truncated && err == nil {
		err = errors.New("helper response exceeds 64 KiB limit")
	}
	if err != nil {
		return nil, fmt.Errorf("ordinary-user %s failed: %w", operation, err)
	}
	return output, nil
}
func (n *Native) Stop(ctx context.Context, plan Plan) error {
	if plan.ServiceFile != ServicePath(plan.Role, plan.Home) {
		return errors.New("unexpected service destination")
	}
	_, exists, err := readManagedFile(plan.ServiceFile)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if plan.Role == "client" && runtime.GOOS == "darwin" {
		_, uid, _, err := accountFor(plan)
		if err != nil {
			return err
		}
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if command(probe, plan, true, "/bin/launchctl", "print", launchDomain(uid)+"/dev.shikigami.agent").Run() != nil {
			return nil
		}
		return run(ctx, plan, true, "/bin/launchctl", "bootout", launchDomain(uid), plan.ServiceFile)
	}
	args := []string{"stop", "shiki-daemon.service"}
	if plan.Role == "client" {
		args = []string{"--user", "stop", "shiki-agent.service"}
	}
	return run(ctx, plan, plan.Role == "client", "/usr/bin/systemctl", args...)
}
func (n *Native) Prepare(ctx context.Context, plan Plan, release string) error {
	_, uid, gid, err := accountFor(plan)
	if err != nil {
		return err
	}
	metadata, err := readRegular(release, ManifestName, 64<<10)
	if err != nil {
		return err
	}
	var manifest Manifest
	if err = strictjson.Decode(metadata, &manifest); err != nil || !releaseVersion.MatchString(manifest.Version) {
		return errors.New("invalid selected release metadata")
	}
	n.ExpectedVersion = manifest.Version
	if plan.Role == "daemon" && os.Geteuid() == 0 {
		// System binaries are root-owned, readable/executable by the daemon user;
		// receipts remain root-private. Configuration/state are separately private.
		for _, directory := range []string{n.Prefix, filepath.Join(n.Prefix, "releases"), release} {
			if err = os.Chmod(directory, 0755); err != nil {
				return err
			}
		}
		for _, name := range append(names(), ManifestName, ChecksumsName) {
			mode := os.FileMode(0644)
			if executable(name) {
				mode = 0755
			}
			if err = os.Chmod(filepath.Join(release, name), mode); err != nil {
				return err
			}
		}
	}
	for _, directory := range []string{filepath.Dir(plan.Config), plan.StateDir, filepath.Dir(plan.PluginsDir)} {
		if err = privateOwnedDir(directory, uid, gid); err != nil {
			return err
		}
	}
	// Link and config creation run with the target UID, including when installed
	// through sudo. User-owned paths cannot redirect a privileged write.
	if _, err = n.userCall(ctx, plan, "config"); err != nil {
		return err
	}
	if plan.Role == "daemon" {
		if err = run(ctx, plan, true, filepath.Join(release, "shiki-daemon"), "--config", plan.Config, "init", "--user", plan.User); err != nil {
			return err
		}
		if err = systemBinDirectory(plan.BinDir); err != nil {
			return err
		}
		for _, name := range []string{"shiki-daemon", "shiki-install"} {
			if err = managedLink(filepath.Join(plan.BinDir, name), filepath.Join(n.Prefix, "current", name)); err != nil {
				return err
			}
		}
	}
	data, err := RenderService(plan, n.Prefix)
	if err != nil {
		return err
	}
	if plan.ServiceFile != ServicePath(plan.Role, plan.Home) {
		return errors.New("unexpected service destination")
	}
	if plan.Role == "client" {
		if err = ownedServiceDirectory(filepath.Dir(plan.ServiceFile), uid); err != nil {
			return err
		}
	}
	if err = writeManagedFile(plan.ServiceFile, data, 0644); err != nil {
		return err
	}
	if plan.Role == "client" {
		if err = os.Chown(plan.ServiceFile, uid, gid); err != nil {
			return err
		}
	}
	return nil
}
func (n *Native) Start(ctx context.Context, plan Plan) error {
	if plan.Role == "client" && runtime.GOOS == "darwin" {
		_, uid, _, err := accountFor(plan)
		if err != nil {
			return err
		}
		return run(ctx, plan, true, "/bin/launchctl", "bootstrap", launchDomain(uid), plan.ServiceFile)
	}
	args := []string{"daemon-reload"}
	if plan.Role == "client" {
		args = append([]string{"--user"}, args...)
	}
	if err := run(ctx, plan, plan.Role == "client", "/usr/bin/systemctl", args...); err != nil {
		return err
	}
	args = []string{"enable", "--now", "shiki-daemon.service"}
	if plan.Role == "client" {
		args = []string{"--user", "enable", "--now", "shiki-agent.service"}
	}
	return run(ctx, plan, plan.Role == "client", "/usr/bin/systemctl", args...)
}
func (n *Native) Probe(ctx context.Context, plan Plan) (string, error) {
	data, err := n.userCall(ctx, plan, "probe")
	defer clear(data)
	clear(n.AuthKey)
	n.AuthKey = nil
	if err != nil {
		return "", err
	}
	var report ProbeReport
	if err = json.Unmarshal(data, &report); err != nil {
		return "", err
	}
	if report.Node == nil || report.State == "" {
		return "", errors.New("invalid ordinary-user readiness response")
	}
	n.Report = &report
	n.LoginURL = ""
	if report.Node.LoginId != "" && report.Node.LoginExpiresAt > time.Now().Unix() && (report.Node.State == "needs_login" || report.Node.State == "authenticating") {
		n.LoginURL = report.Node.LoginUrl
	}
	if report.State != "local_application_ready" {
		return report.State, nil
	}
	if plan.Role == "daemon" && n.IssueToken {
		if err = n.issueToken(ctx, plan); err != nil {
			return "", err
		}
		n.IssueToken = false
		n.TokenIssued = true
	}
	return "local_application_ready", nil
}

func (n *Native) issueToken(parent context.Context, plan Plan) error {
	if n.TokenOutput == "" && n.TokenTTY == nil {
		return errors.New("Token issuance requires a controlling TTY or a new private output file")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	args := []string{"--config", plan.Config, "token", "create"}
	if n.TokenOutput != "" {
		args = append(args, "--output", n.TokenOutput)
	}
	cmd := command(ctx, plan, true, filepath.Join(n.Prefix, "current", "shiki-daemon"), args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if n.TokenOutput == "" {
		cmd.Stdout = n.TokenTTY
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("device Token issuance failed: %w", err)
	}
	return nil
}

func (n *Native) Describe(result *Result) {
	if n.Report == nil {
		return
	}
	report := n.Report
	result.Reason = report.Reason
	result.NodeState, result.NodeReason = report.Node.State, report.Node.ReasonCode
	result.NodeAddresses = append([]string(nil), report.Node.Addresses...)
	result.ApplicationState = "not_checked"
	if report.State == "application_pending" || report.State == "local_application_ready" {
		result.ApplicationState = report.State
	}
	result.ExternalPeerAcceptance = "pending"
	if report.Readiness != nil {
		result.DaemonID = report.Readiness.System.GetDaemonId()
		result.Capabilities = append([]string(nil), report.Readiness.System.GetCapabilities()...)
		result.ClaudeVersion = report.Readiness.ClaudeVersion.GetVersion()
		result.ClaudeLoginState = report.Readiness.ClaudeLoginState
	}
	result.TokenIssued = n.TokenIssued
	if n.TokenIssued {
		result.TokenDestination = n.TokenOutput
		if result.TokenDestination == "" {
			result.TokenDestination = "controlling_tty"
		}
	}
}
func (n *Native) RemoveService(ctx context.Context, plan Plan) error {
	if plan.ServiceFile != ServicePath(plan.Role, plan.Home) {
		return errors.New("unexpected service destination")
	}
	_, exists, err := readManagedFile(plan.ServiceFile)
	if err != nil {
		return err
	}
	if exists {
		if runtime.GOOS == "linux" {
			args := []string{"disable", "shiki-daemon.service"}
			if plan.Role == "client" {
				args = []string{"--user", "disable", "shiki-agent.service"}
			}
			if err = run(ctx, plan, plan.Role == "client", "/usr/bin/systemctl", args...); err != nil {
				return err
			}
		}
		parent, err := os.OpenRoot(filepath.Dir(plan.ServiceFile))
		if err != nil {
			return err
		}
		err = parent.Remove(filepath.Base(plan.ServiceFile))
		parent.Close()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	// Cleanup must also work before bootstrap succeeded, without executing a
	// possibly inaccessible helper through the failed initial current link.
	if err = removeUserLinks(plan, n.Prefix); err != nil {
		return err
	}
	if plan.Role == "daemon" {
		if _, err = os.Stat(plan.BinDir); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		if err = systemBinDirectory(plan.BinDir); err != nil {
			return err
		}
		for _, name := range []string{"shiki-daemon", "shiki-install"} {
			if err = unlinkManaged(filepath.Join(plan.BinDir, name), filepath.Join(n.Prefix, "current", name)); err != nil {
				return err
			}
		}
	}
	return nil
}
