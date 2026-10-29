package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
)

const installerHelp = `Usage: shiki-install <command> [flags]

Short-lived local installation and maintenance helper.

Commands:
  install    Verify and install a fixed local bundle, or stage it without activation
  rollback   Select the previous compatible artifact; retain current data and credentials
  uninstall  Remove managed activation and links; retain configuration, state and projects
  status     Print the installation receipt; not an application readiness check
  probe      Check local node and application readiness; plain probe issues no Token
  version    Print the helper build version
  help       Show this help or help for one command

Use shiki-install <command> --help for flags.
Exit codes: 0 operation completed, 1 operation failed, 2 invalid arguments,
3 login or local application conditions remain pending.
Local checksums are not publisher signatures. External peer acceptance remains separate.
`

func publicOperation(operation string) bool {
	switch operation {
	case "install", "rollback", "uninstall", "status", "probe", "version":
		return true
	default:
		return false
	}
}

func writeHelp(out io.Writer, help string) (int, error) {
	if _, err := io.WriteString(out, help); err != nil {
		return 1, err
	}
	return 0, nil
}

func commandHelp(out io.Writer, operation string, flags *flag.FlagSet) (int, error) {
	var help bytes.Buffer
	fmt.Fprintf(&help, "Usage: shiki-install %s [flags]\n\n", operation)
	if operation == "install" {
		help.WriteString("Requires --bundle, --version and an independently trusted --checksums-sha256 pin.\nUse --stage-only for offline artifact staging without service activation.\n")
	} else {
		help.WriteString("Select the existing installation with --role and --prefix; managed paths and\nservice settings come from its receipt. Layout overrides are rejected.\n")
	}
	if operation == "install" || operation == "probe" {
		help.WriteString("Token issuance is limited to daemon install/probe; use a new --token-output\nfile in non-interactive mode. Plain probe never issues a Token.\n")
	}
	help.WriteString("\nFlags:\n")
	visible := flag.NewFlagSet(operation, flag.ContinueOnError)
	visible.SetOutput(&help)
	flags.VisitAll(func(option *flag.Flag) {
		if operationFlag(operation, option.Name) {
			visible.Var(option.Value, option.Name, option.Usage)
			visible.Lookup(option.Name).DefValue = option.DefValue
		}
	})
	visible.PrintDefaults()
	help.WriteString("\n  -h, --help\n    Show help without reading credentials, inspecting installations or activating services.\n")
	return writeHelp(out, help.String())
}

// Flags that cannot affect this operation are rejected instead of silently
// changing expectations (especially stage-only on destructive maintenance).
func operationFlag(operation, name string) bool {
	if operation == "install" {
		return true
	}
	switch name {
	case "role", "user", "prefix", "non-interactive":
		return true
	case "tailscale-key-file", "token-output", "issue-token":
		return operation == "probe"
	default:
		return false
	}
}
