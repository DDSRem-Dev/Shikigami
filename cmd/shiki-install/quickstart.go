package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/install"
	"golang.org/x/term"
)

type quickstartOptions struct {
	Role, User, Components, Bundle, Archive, Signature, PublicKey, Version, Pin string
	Resume                                                                      bool
}

const quickstartHelp = `Usage: shiki-install quickstart --role daemon|client --bundle PATH --version VERSION --checksums-sha256 PIN [flags]

Interactive guided installation. Normally invoked by the authenticated bootstrap.
Reads prompts and credentials only through /dev/tty. Existing identities, project
data and configuration are retained. Repeating the command resumes onboarding;
interrupted artifact transactions still require the installer's explicit rollback.

  --components all|core  all (default) includes optional region/browser support
  --user NAME            daemon account; defaults to the dedicated shiki account
  --archive PATH --signature PATH --public-key PATH
                         authenticated release inputs for Linux system support
  --resume               explicitly resume the same guided workflow

System changes require confirmation. Account authorization remains interactive.
Exit 3 means installed but login/application conditions remain pending, not ready.
`

func parseQuickstart(args []string, out io.Writer) (quickstartOptions, bool, error) {
	var o quickstartOptions
	f := flag.NewFlagSet("quickstart", flag.ContinueOnError)
	f.SetOutput(out)
	f.Usage = func() { _, _ = io.WriteString(out, quickstartHelp) }
	f.StringVar(&o.Role, "role", "client", "daemon or client")
	f.StringVar(&o.User, "user", "", "ordinary daemon account")
	f.StringVar(&o.Components, "components", "all", "all or core")
	f.StringVar(&o.Bundle, "bundle", "", "authenticated extracted bundle")
	f.StringVar(&o.Archive, "archive", "", "signed archive")
	f.StringVar(&o.Signature, "signature", "", "archive signature")
	f.StringVar(&o.PublicKey, "public-key", "", "independent root-owned trust key")
	f.StringVar(&o.Version, "version", "", "fixed release")
	f.StringVar(&o.Pin, "checksums-sha256", "", "authenticated checksum pin")
	f.BoolVar(&o.Resume, "resume", false, "resume guided onboarding")
	if err := f.Parse(args); err != nil {
		return o, errors.Is(err, flag.ErrHelp), err
	}
	if f.NArg() != 0 {
		return o, false, errors.New("quickstart accepts flags only")
	}
	if o.Role != "daemon" && o.Role != "client" {
		return o, false, errors.New("--role must be daemon or client")
	}
	if o.Components != "all" && o.Components != "core" {
		return o, false, errors.New("--components must be all or core")
	}
	if !filepath.IsAbs(o.Bundle) || o.Version == "" || o.Pin == "" {
		return o, false, errors.New("quickstart requires an absolute authenticated bundle, version and checksum pin")
	}
	if o.User != "" && !regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`).MatchString(o.User) {
		return o, false, errors.New("quickstart requires a conventional ordinary account name; use advanced install for custom layouts")
	}
	if o.Role == "client" && o.User != "" {
		return o, false, errors.New("client quickstart uses the current ordinary user; omit --user")
	}
	if o.Role == "daemon" && o.Components == "all" {
		for _, path := range []string{o.Archive, o.Signature, o.PublicKey} {
			if !filepath.IsAbs(path) {
				return o, false, errors.New("all-components daemon quickstart requires absolute --archive, --signature and --public-key")
			}
		}
	}
	return o, false, nil
}

func executeQuickstart(ctx context.Context, args []string, out, errOut io.Writer) (int, error) {
	o, help, err := parseQuickstart(args, out)
	if help {
		return 0, nil
	}
	if err != nil {
		return 2, err
	}
	if o.Role == "daemon" {
		if runtime.GOOS != "linux" || os.Geteuid() != 0 {
			return 2, errors.New("daemon quickstart requires root on Debian 12 or Ubuntu 24.04 with systemd")
		}
		if err = quickstartLinuxPlatform(); err != nil {
			return 2, err
		}
		if o.User == "" {
			o.User = "shiki"
		}
	} else if os.Geteuid() == 0 {
		return 2, errors.New("run client quickstart as your ordinary user, without sudo")
	}
	// Authenticate all payloads before account creation, downloads or packages.
	ctx = install.WithProgress(ctx, errOut)
	if err = install.ProgressStep(ctx, "Verifying installation bundle", func() error {
		_, err := install.Verify(o.Bundle, o.Version, o.Pin)
		return err
	}); err != nil {
		return 1, err
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return 2, errors.New("quickstart requires an interactive terminal; use advanced install for automation")
	}
	defer tty.Close()
	if !term.IsTerminal(int(tty.Fd())) {
		return 2, errors.New("quickstart requires a controlling terminal")
	}
	ctx = install.WithProgress(ctx, tty)
	if err = quickstartPlan(ctx, o, tty); err != nil {
		return 1, err
	}
	if o.Role == "daemon" {
		return quickstartDaemon(ctx, o, tty, out, errOut)
	}
	account, err := user.Current()
	if err != nil {
		return 1, err
	}
	plan := install.Plan{Role: "client", User: account.Username, Port: 9527, Claude: "claude"}
	prefix := ""
	if err = defaults(&plan, &prefix); err != nil {
		return 1, err
	}
	if err = quickstartExistingPlan(prefix, plan); err != nil {
		return 1, err
	}
	code, err := quickstartInstall(ctx, o, plan, "", out, errOut)
	if err != nil || (code != 0 && code != 3) {
		return code, err
	}
	if err = quickstartShellPath(plan.Home); err != nil {
		fmt.Fprintf(tty, "PATH setup left your shell files unchanged where unsafe: %v\nUse %s/shiki; connection setup continues now.\n", err, plan.BinDir)
	} else {
		fmt.Fprintf(tty, "CLI installed at %s/shiki. New terminals using your configured shell will include it in PATH.\n", plan.BinDir)
	}
	// setup owns node login, connection, plugin admission and actual peer checks.
	err = quickstartCommand(ctx, plan, true, tty, nil, filepath.Join(prefix, "current", "shiki"), "setup", "--components="+o.Components)
	if err != nil {
		return quickstartExit(err), err
	}
	return 0, nil
}

func quickstartPlan(ctx context.Context, o quickstartOptions, tty *os.File) error {
	fmt.Fprintf(tty, "\nShikigami %s — guided %s installation (%s)\n", o.Version, o.Role, o.Components)
	if o.Role == "daemon" {
		fmt.Fprintf(tty, "Run as ordinary account %s (create it if absent). Install the managed daemon and tmux.\n", o.User)
		fmt.Fprintln(tty, "If Claude is absent, download https://claude.ai/install.sh and run its official installer as that ordinary user. Existing Claude is retained.")
		fmt.Fprintln(tty, "Use the embedded Tailscale node; no public listener or system tailscaled changes. Deliver one private device Token for connecting your Mac, even if Claude login remains pending.")
		if o.Components == "all" {
			fmt.Fprintln(tty, "Install signed region/browser support, distribution packages and official Google Chrome stable; enable the user's systemd manager/linger and the restricted region helper.")
			fmt.Fprintln(tty, "Chrome policy affects ALL official Chrome profiles on this host: password saving, Chrome sync and Chrome-account sign-in are disabled; website sign-in remains available.")
			fmt.Fprintln(tty, "Create a dedicated encrypted keyring for a new browser setup and enroll systemd host-key credentials for unattended unlock. Existing enrollment is reused, never reset. Root/whole-disk access can defeat host-key encryption.")
			fmt.Fprintln(tty, "The exact system-support plan is printed before applying. Package changes are not automatically rolled back. No country, DNS, browser boot profile or project-sync direction is chosen for you.")
		}
	} else {
		fmt.Fprintln(tty, "Install/update your private agent and login service, add a fixed ~/.local/bin PATH block to zsh/bash startup files, then guide Tailscale login, server connection and selected plugins. Existing configuration and data are retained.")
	}
	fmt.Fprint(tty, "Continue with this installation? [y/N] ")
	answer, err := client.ReadConsoleLine(ctx, tty, 32)
	if err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes" {
		return errors.New("installation cancelled; no changes made")
	}
	return nil
}

func quickstartInstall(ctx context.Context, o quickstartOptions, plan install.Plan, token string, out, errOut io.Writer) (int, error) {
	args := []string{"install", "--role=" + o.Role, "--bundle=" + o.Bundle, "--version=" + o.Version, "--checksums-sha256=" + o.Pin}
	if o.Role == "daemon" {
		args = append(args, "--user="+plan.User, "--claude-executable="+plan.Claude, "--path="+plan.Path)
	}
	if token != "" {
		args = append(args, "--token-output="+token)
	}
	var code int
	err := install.ProgressStep(ctx, "Installing application files and checking managed services", func() error {
		var err error
		code, err = execute(ctx, args, out, errOut)
		return err
	})
	return code, err
}

func quickstartExistingPlan(prefix string, want install.Plan) error {
	data, err := quickstartPrivateRead(filepath.Join(prefix, "installation.json"), uint32(os.Geteuid()), 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var receipt install.Receipt
	if err = json.Unmarshal(data, &receipt); err != nil {
		return err
	}
	if receipt.Pending != nil {
		return errors.New("an interrupted artifact transaction requires explicit shiki-install rollback; quickstart preserved it")
	}
	if receipt.Active.Release != "" && receipt.Plan != want {
		return errors.New("existing installation has custom settings; use advanced install with its recorded layout (nothing was overwritten)")
	}
	return nil
}
