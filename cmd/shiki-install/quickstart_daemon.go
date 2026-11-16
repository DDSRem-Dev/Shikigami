package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/install"
)

func quickstartDaemon(ctx context.Context, o quickstartOptions, tty *os.File, out, errOut io.Writer) (int, error) {
	account, err := user.Lookup(o.User)
	if err != nil {
		var unknown user.UnknownUserError
		if !errors.As(err, &unknown) {
			return 1, err
		}
		if err = install.ProgressStep(ctx, "Creating the ordinary daemon account", func() error {
			return quickstartCommand(ctx, install.Plan{}, false, tty, nil, "/usr/sbin/useradd", "--create-home", "--shell", "/bin/bash", "--", o.User)
		}); err != nil {
			return 1, err
		}
		account, err = user.Lookup(o.User)
		if err != nil {
			return 1, err
		}
	}
	if account.Uid == "0" {
		return 2, errors.New("daemon runtime account cannot be root")
	}
	plan := install.Plan{Role: "daemon", User: o.User, Claude: filepath.Join(account.HomeDir, ".local", "bin", "claude"), Port: 9527}
	prefix := ""
	if err = defaults(&plan, &prefix); err != nil {
		return 1, err
	}
	// Retain the original executable and PATH on a standard existing deployment.
	receipt, err := quickstartReceipt(prefix)
	if err != nil {
		return 1, err
	}
	if receipt != nil && receipt.Active.Release != "" {
		plan.Claude, plan.Path = receipt.Plan.Claude, receipt.Plan.Path
	}
	if err = quickstartExistingPlan(prefix, plan); err != nil {
		return 1, err
	}
	if err = quickstartDependencies(ctx, plan, tty); err != nil {
		return 1, err
	}
	if o.Components == "all" {
		if err = quickstartSystemSupport(ctx, o, plan, tty); err != nil {
			return 1, err
		}
	}
	// The fixed output file makes a lost response/repeated wizard incapable of
	// silently minting multiple credentials. The daemon creates it as its UID.
	tokenPath := filepath.Join(plan.StateDir, "quickstart-device-token")
	initialToken := ""
	if receipt == nil || receipt.Active.Release == "" {
		initialToken = tokenPath
	}
	code, err := quickstartInstall(ctx, o, plan, initialToken, out, errOut)
	if err != nil || (code != 0 && code != 3) {
		return code, err
	}
	addresses, err := quickstartNode(ctx, plan, prefix, tty)
	if err != nil {
		return 3, fmt.Errorf("service installed; node authorization remains pending: %w", err)
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return 1, err
	}
	token, tokenPath, err := quickstartPairingToken(ctx, plan, prefix, tokenPath, uint32(uid), tty)
	if err != nil {
		return 1, err
	}
	defer clear(token)
	if len(bytes.TrimSpace(token)) == 0 {
		return 1, errors.New("existing pairing credential is empty; preserved for review")
	}
	fmt.Fprintln(tty, "\nConnect your Mac using its guided installer:")
	for _, address := range addresses {
		fmt.Fprintf(tty, "  Shiki node address: %s (port %d)\n", address, plan.Port)
	}
	fmt.Fprintln(tty, "  Private device Token (paste only into the Mac connection prompt):")
	_, _ = tty.Write(token)
	fmt.Fprintf(tty, "\nThe same credential is retained privately at %s; repeating setup does not create another.\n", tokenPath)
	code, err = execute(ctx, []string{"probe", "--role=daemon", "--user=" + plan.User}, out, errOut)
	if code == 3 {
		fmt.Fprintln(tty, "Server connection is available; application readiness is still pending. Finish Claude authorization through your remote terminal/browser, then rerun this command. Mac onboarding can continue now.")
	} else if err == nil && code == 0 {
		fmt.Fprintln(tty, "Server local checks passed. Run the Mac installer to connect and verify the selected plugins across both machines.")
	}
	return code, err
}

func quickstartReceipt(prefix string) (*install.Receipt, error) {
	data, err := quickstartPrivateRead(filepath.Join(prefix, "installation.json"), uint32(os.Geteuid()), 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt install.Receipt
	if err = json.Unmarshal(data, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func quickstartDependencies(ctx context.Context, plan install.Plan, tty *os.File) error {
	if _, err := os.Stat("/usr/bin/tmux"); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(tty, "Installing tmux and HTTPS download dependencies from your configured distribution repositories…")
		if err = quickstartCommand(ctx, plan, false, tty, nil, "/usr/bin/apt-get", "-o", "APT::Update::Error-Mode=any", "update"); err != nil {
			return err
		}
		if err = quickstartCommand(ctx, plan, false, tty, nil, "/usr/bin/apt-get", "install", "--yes", "--no-install-recommends", "tmux", "ca-certificates", "curl"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	found := false
	paths := []string{plan.Claude}
	if !filepath.IsAbs(plan.Claude) {
		paths = nil
		for _, directory := range filepath.SplitList(plan.Path) {
			paths = append(paths, filepath.Join(directory, plan.Claude))
		}
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			found = true
			break
		}
	}
	if found {
		return nil
	}
	if plan.Claude != filepath.Join(plan.Home, ".local", "bin", "claude") {
		return errors.New("configured Claude executable is missing; preserve your existing layout and repair it before resuming")
	}
	fmt.Fprintln(tty, "Installing Claude from its official HTTPS installer as the ordinary runtime user…")
	var script []byte
	err := install.ProgressStep(ctx, "Downloading the official Claude installer", func() error {
		var err error
		script, err = quickstartClaudeScript(ctx, nil)
		return err
	})
	if err != nil {
		return err
	}
	defer clear(script)
	if err = quickstartCommand(ctx, plan, true, tty, bytes.NewReader(script), "/bin/bash", "--noprofile", "--norc", "-s"); err != nil {
		return err
	}
	return dependencies(plan)
}

func quickstartSystemSupport(ctx context.Context, o quickstartOptions, plan install.Plan, tty *os.File) error {
	options := install.ExtensionOptions{Component: "all", User: plan.User, Bundle: o.Bundle, Version: o.Version, Archive: o.Archive, Signature: o.Signature, PublicKey: o.PublicKey, BootUnlock: true, KeyringMode: "dedicated", Yes: true}
	var status install.QuickstartSupport
	err := install.ProgressStep(ctx, "Verifying signed system support and checking previous installation", func() error {
		var err error
		status, err = install.InspectQuickstartSupport(ctx, options)
		return err
	})
	if err != nil {
		return err
	}
	if status.Current {
		fmt.Fprintln(tty, "Signed system support and encrypted-keyring enrollment are already current; retaining running Chrome and skipping package/enrollment changes.")
		return nil
	}
	if status.KeyringMode != "" {
		options.KeyringMode, options.ReuseKeyringCredential, options.ProvisionKeyring = status.KeyringMode, status.ReuseCredential, status.ProvisionKeyring
	} else {
		// Existing non-wizard browser profiles may use the login keyring. Never
		// silently migrate their encryption context to a new dedicated store.
		for _, path := range []string{filepath.Join(plan.StateDir, "plugin-data", "browser", "profiles.json"), filepath.Join(plan.Home, ".local", "share", "shikigami", "browser-keyring")} {
			if _, statErr := os.Lstat(path); statErr == nil {
				return errors.New("existing browser/keyring state needs explicit administrator setup; quickstart will not change its encryption context")
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
		}
		options.ProvisionKeyring = true
	}
	if !options.ReuseKeyringCredential {
		options.ReadKeyringSecret = keyringSecretReader(plan.User, false, "", options.ProvisionKeyring)
	}
	var support *install.ExtensionPlan
	err = install.ProgressStep(ctx, "Preparing the system package and keyring plan", func() error {
		var err error
		support, err = install.ExtensionPlatformPlan(ctx, "setup", options)
		return err
	})
	if err != nil {
		return err
	}
	if len(support.Blocked) != 0 {
		return fmt.Errorf("system support cannot be installed: %s", strings.Join(support.Blocked, "; "))
	}
	return quickstartConfirmSystemPlan(ctx, tty, support, func() error { return install.ApplyExtensionPlan(ctx, "setup", options) })
}

func quickstartConfirmSystemPlan(ctx context.Context, tty *os.File, support *install.ExtensionPlan, apply func() error) error {
	encoder := json.NewEncoder(tty)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(support); err != nil {
		return err
	}
	fmt.Fprint(tty, "Apply this exact system-support and keyring plan? [y/N] ")
	answer, err := client.ReadConsoleLine(ctx, tty, 32)
	if err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes" {
		return errors.New("system-support setup cancelled; rerun to continue")
	}
	return apply()
}
