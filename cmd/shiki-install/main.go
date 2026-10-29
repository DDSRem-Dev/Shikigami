// shiki-install is a short-lived installation helper, not another resident host.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code, err := execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shiki-install:", err)
	}
	if code != 0 {
		os.Exit(code)
	}
}
func execute(ctx context.Context, args []string, out, errOut io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, errors.New("use install, rollback, uninstall, status, probe, or version")
	}
	if args[0] == "help" {
		if len(args) == 1 {
			return writeHelp(out, installerHelp)
		}
		if len(args) != 2 || !publicOperation(args[1]) {
			return 2, errors.New("use help or help <install|rollback|uninstall|status|probe|version>")
		}
		args = []string{args[1], "--help"}
	}
	operation := args[0]
	if operation == "--help" || operation == "-h" {
		if len(args) != 1 {
			return 2, errors.New("unexpected arguments after help")
		}
		return writeHelp(out, installerHelp)
	}
	if operation == "version" || operation == "--version" {
		if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
			return writeHelp(out, "Usage: shiki-install version\nPrint the helper build version.\n")
		}
		if len(args) != 1 {
			return 2, errors.New("version accepts no arguments")
		}
		_, err := fmt.Fprintln(out, buildinfo.String("shiki-install"))
		if err != nil {
			return 1, err
		}
		return 0, nil
	}
	if strings.HasPrefix(operation, "_user-") {
		err := install.UserHelper(ctx, strings.TrimPrefix(operation, "_user-"), os.Stdin, out)
		if err != nil {
			return 1, err
		}
		return 0, nil
	}
	if !publicOperation(operation) {
		return 2, errors.New("unknown installation command; use shiki-install --help")
	}
	flags := flag.NewFlagSet("shiki-install "+operation, flag.ContinueOnError)
	flags.SetOutput(errOut)
	flags.Usage = func() {} // Explicit help goes to stdout before any filesystem or account access.
	var plan install.Plan
	var prefix, bundle, version, pin, keyFile, tokenOutput string
	var stage, noninteractive, issueToken bool
	flags.StringVar(&plan.Role, "role", "client", "client or daemon")
	flags.StringVar(&plan.User, "user", "", "existing ordinary account")
	flags.StringVar(&prefix, "prefix", "", "managed installation directory")
	flags.StringVar(&bundle, "bundle", "", "absolute extracted local bundle directory")
	flags.StringVar(&version, "version", "", "fixed version required for install")
	flags.StringVar(&pin, "checksums-sha256", "", "SHA256SUMS digest obtained independently from a trusted source")
	flags.StringVar(&plan.Config, "config", "", "absolute configuration path")
	flags.StringVar(&plan.StateDir, "state-dir", "", "private host state directory")
	flags.StringVar(&plan.PluginsDir, "plugins-dir", "", "managed plugin link location")
	flags.StringVar(&plan.BinDir, "bin-dir", "", "command link directory")
	flags.StringVar(&plan.ProjectsDir, "projects-dir", "", "daemon project base")
	flags.StringVar(&plan.Claude, "claude-executable", "claude", "daemon Claude executable")
	flags.StringVar(&plan.Path, "path", "", "explicit service executable search path")
	flags.IntVar(&plan.Port, "port", 9527, "tailnet Connect port")
	flags.StringVar(&plan.TLSAddress, "direct-tls-address", "", "optional direct TLS host:port")
	flags.StringVar(&plan.TLSCert, "tls-cert", "", "direct TLS certificate")
	flags.StringVar(&plan.TLSKey, "tls-key", "", "direct TLS private key")
	flags.StringVar(&keyFile, "tailscale-key-file", "", "one-time key from a private regular file; never argv or config")
	flags.StringVar(&tokenOutput, "token-output", "", "new private output file for initial daemon Token")
	flags.BoolVar(&issueToken, "issue-token", false, "explicitly issue a daemon device Token after local readiness; plain probe never issues one")
	flags.BoolVar(&stage, "stage-only", false, "verify and stage artifacts; do not activate, initialize, or start services")
	flags.BoolVar(&noninteractive, "non-interactive", false, "never open or read a terminal")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return commandHelp(out, operation, flags)
		}
		return 2, err
	}
	if flags.NArg() != 0 {
		return 2, errors.New("unexpected positional arguments")
	}
	var unsupported string
	flags.Visit(func(option *flag.Flag) {
		if unsupported == "" && !operationFlag(operation, option.Name) {
			unsupported = option.Name
		}
	})
	if unsupported != "" {
		return 2, fmt.Errorf("--%s does not apply to %s; use shiki-install help %s", unsupported, operation, operation)
	}
	if err := defaults(&plan, &prefix); err != nil {
		return 2, err
	}
	if tokenOutput != "" && (!filepath.IsAbs(tokenOutput) || strings.ContainsAny(tokenOutput, "\x00\r\n")) {
		return 2, errors.New("--token-output must be an absolute path without control characters")
	}
	if issueToken || tokenOutput != "" {
		if plan.Role != "daemon" || (operation != "install" && operation != "probe") || stage {
			return 2, errors.New("Token issuance applies only to daemon install/probe activation")
		}
		if noninteractive && tokenOutput == "" {
			return 2, errors.New("non-interactive Token issuance requires --token-output")
		}
	}
	native := &install.Native{Prefix: prefix, Interactive: !noninteractive, TokenOutput: tokenOutput, IssueToken: issueToken || tokenOutput != ""}
	var err error
	if keyFile != "" {
		native.AuthKey, err = readKey(keyFile, plan.User)
		if err != nil {
			return 1, err
		}
		defer clear(native.AuthKey)
	}
	if operation == "install" {
		if err := validate(plan, prefix, stage); err != nil {
			return 2, err
		}
	}
	var verified *install.Bundle
	if operation == "install" {
		verified, err = install.Verify(bundle, version, pin)
		if err != nil {
			return 1, err
		}
		if !stage {
			if err = dependencies(plan); err != nil {
				return 1, err
			}
		}
	}
	if operation != "install" {
		if _, err = os.Lstat(prefix); err != nil {
			return 1, err
		}
	}
	artifacts, err := install.OpenArtifacts(prefix)
	if err != nil {
		return 1, err
	}
	defer artifacts.Close()
	receipt, err := artifacts.Receipt()
	if err != nil {
		return 1, err
	}
	if operation == "status" {
		err = json.NewEncoder(out).Encode(receipt)
		return 0, err
	}
	if operation == "probe" {
		if receipt.Pending != nil {
			return 1, errors.New("interrupted installation requires explicit rollback before probing or issuing a Token")
		}
		current, err := artifacts.Current()
		if err != nil {
			return 1, err
		}
		if current != receipt.Active.Release {
			return 1, errors.New("active link differs from installation receipt")
		}
		native.ExpectedVersion = receipt.Active.Version
	}
	if operation != "install" {
		plan = receipt.Plan
	}
	if operation == "install" && !stage && receipt.Active.Release == "" {
		if _, err := os.Lstat(plan.ServiceFile); err == nil {
			return 1, errors.New("existing service has no installation receipt; preserve it and migrate explicitly before activation")
		} else if !errors.Is(err, os.ErrNotExist) {
			return 1, err
		}
	}
	if operation == "install" && !stage && receipt.Active.Release == "" && plan.Role == "daemon" {
		native.IssueToken = true
		if noninteractive && tokenOutput == "" {
			return 2, errors.New("first non-interactive daemon activation requires --token-output")
		}
	}
	if operation == "install" && !stage && noninteractive && receipt.Active.Release == "" && len(native.AuthKey) == 0 {
		existing, err := install.ExistingConfiguration(plan)
		if err != nil {
			return 1, err
		}
		if !existing {
			return 2, errors.New("fresh non-interactive configuration requires --tailscale-key-file; use --stage-only for offline staging")
		}
	}
	if operation == "install" && !stage && native.IssueToken && receipt.Active.Release != "" && receipt.Active.Version == verified.Manifest.Version && receipt.Active.Checksums == verified.TrustedChecksums {
		return 2, errors.New("unchanged installation does not issue Tokens; use probe --issue-token or probe --token-output explicitly")
	}
	var tty *os.File
	if !stage && !noninteractive {
		tty, err = os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return 2, errors.New("interactive installation requires a TTY; pass --non-interactive with required inputs")
		}
		defer tty.Close()
		native.TokenTTY = tty
	}
	var result *install.Result
	switch operation {
	case "install":
		result, err = artifacts.Install(ctx, verified, plan, stage, native)
	case "rollback":
		result, err = artifacts.Rollback(ctx, native)
	case "uninstall":
		result, err = artifacts.Uninstall(ctx, native)
	case "probe":
		if receipt.Active.Release == "" {
			return 1, errors.New("no active installation")
		}
		if _, err = install.Verify(filepath.Join(prefix, receipt.Active.Release), receipt.Active.Version, receipt.Active.Checksums); err != nil {
			return 1, err
		}
		var state string
		state, err = native.Probe(ctx, plan)
		result = &install.Result{State: state, Version: receipt.Active.Version, Prefix: prefix, Reason: "REMOTE_APPLICATION_ACCEPTANCE_REQUIRED"}
	}
	if err != nil {
		return 1, err
	}
	if native.LoginURL != "" && tty != nil {
		fmt.Fprintln(tty, "Complete node authentication in your browser:", native.LoginURL)
	}
	native.Describe(result)
	if err = json.NewEncoder(out).Encode(result); err != nil {
		return 1, err
	}
	if strings.HasPrefix(result.State, "node_") || result.State == "application_pending" {
		return 3, nil
	}
	return 0, nil
}
func defaults(plan *install.Plan, prefix *string) error {
	if plan.Role != "client" && plan.Role != "daemon" {
		return errors.New("--role must be client or daemon")
	}
	if plan.User == "" {
		account, err := user.Current()
		if err != nil {
			return err
		}
		if plan.Role == "daemon" || account.Uid == "0" {
			return errors.New("--user must explicitly name an existing ordinary daemon user")
		}
		plan.User = account.Username
	}
	account, err := user.Lookup(plan.User)
	if err != nil {
		return err
	}
	if account.Uid == "0" {
		return errors.New("root is not a supported runtime user")
	}
	plan.Home = account.HomeDir
	if plan.Role == "client" && (os.Geteuid() == 0 || account.Uid != fmt.Sprint(os.Geteuid())) {
		return errors.New("install the client as its ordinary user, without sudo")
	}
	if plan.Role == "daemon" {
		if *prefix == "" {
			*prefix = "/opt/shiki"
		}
		if plan.Config == "" {
			plan.Config = "/etc/shiki/daemon.yaml"
		}
		if plan.StateDir == "" {
			plan.StateDir = "/var/lib/shiki"
		}
		if plan.BinDir == "" {
			plan.BinDir = "/usr/local/bin"
		}
		if plan.ProjectsDir == "" {
			plan.ProjectsDir = filepath.Join(plan.Home, "projects")
		}
		if plan.PluginsDir == "" {
			plan.PluginsDir = filepath.Join(plan.StateDir, "plugins")
		}
		plan.TailscaleDir = filepath.Join(plan.StateDir, "tailscale")
	} else {
		if *prefix == "" {
			*prefix = filepath.Join(plan.Home, ".local", "share", "shiki", "install")
		}
		if plan.Config == "" {
			plan.Config = filepath.Join(plan.Home, ".shiki", "config.yaml")
		}
		if plan.StateDir == "" {
			plan.StateDir = filepath.Join(plan.Home, ".shiki", "agent")
		}
		if plan.BinDir == "" {
			plan.BinDir = filepath.Join(plan.Home, ".local", "bin")
		}
		if plan.PluginsDir == "" {
			plan.PluginsDir = filepath.Join(plan.Home, ".shiki", "plugins")
		}
		plan.TailscaleDir = filepath.Join(filepath.Dir(plan.Config), "tailscale")
	}
	if plan.Path == "" {
		plan.Path = filepath.Join(plan.Home, ".local", "bin") + ":/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"
	}
	plan.ServiceFile = install.ServicePath(plan.Role, plan.Home)
	return nil
}
func validate(plan install.Plan, prefix string, stage bool) error {
	for _, path := range []string{prefix, plan.Config, plan.StateDir, plan.TailscaleDir, plan.PluginsDir, plan.BinDir, plan.Home} {
		if !filepath.IsAbs(path) || filepath.Clean(path) == "/" || strings.ContainsAny(path, "\x00\r\n") {
			return errors.New("installation paths must be absolute, non-root and free of control characters")
		}
	}
	if plan.Port < 1 || plan.Port > 65535 {
		return errors.New("port must be in 1..65535")
	}
	tls := 0
	for _, value := range []string{plan.TLSAddress, plan.TLSCert, plan.TLSKey} {
		if value != "" {
			tls++
		}
	}
	if tls != 0 && tls != 3 {
		return errors.New("direct TLS address/certificate/key must be supplied together")
	}
	if !stage && plan.Role == "daemon" && (runtime.GOOS != "linux" || os.Geteuid() != 0) {
		return errors.New("daemon activation requires Linux and root installation privileges; runtime remains the configured ordinary user")
	}
	if _, err := install.RenderService(plan, prefix); err != nil {
		return err
	}
	return nil
}
func dependencies(plan install.Plan) error {
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/run/systemd/system"); err != nil {
			return errors.New("systemd is not running; use --stage-only and explicit agent start/stop")
		}
	}
	if plan.Role == "daemon" {
		for _, name := range []string{"tmux", plan.Claude} {
			found := false
			if filepath.IsAbs(name) {
				info, err := os.Stat(name)
				found = err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
			} else {
				for _, directory := range filepath.SplitList(plan.Path) {
					info, err := os.Stat(filepath.Join(directory, name))
					if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
						found = true
						break
					}
				}
			}
			if !found {
				return fmt.Errorf("dependency %q is missing from the explicit service PATH; install it through its official process", name)
			}
		}
	}
	return nil
}
func readKey(path, username string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("key file path must be absolute")
	}
	account, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > 4096 || (fmt.Sprint(owner.Uid) != account.Uid && owner.Uid != uint32(os.Geteuid())) {
		return nil, errors.New("key file must be a private owned regular file no larger than 4 KiB")
	}
	key, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return nil, err
	}
	if len(key) > 4096 {
		return nil, errors.New("key file exceeds limit")
	}
	return bytesTrimSpace(key), nil
}
func bytesTrimSpace(data []byte) []byte { // keep a single backing buffer that callers can clear
	start, end := 0, len(data)
	for start < end && (data[start] == ' ' || data[start] == '\n' || data[start] == '\r' || data[start] == '\t') {
		start++
	}
	for end > start && (data[end-1] == ' ' || data[end-1] == '\n' || data[end-1] == '\r' || data[end-1] == '\t') {
		end--
	}
	copy(data, data[start:end])
	clear(data[end-start:])
	return data[:end-start]
}
