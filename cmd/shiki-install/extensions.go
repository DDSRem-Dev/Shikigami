package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
)

const extensionInstallerHelp = `System plugin support is optional and is never installed by enabling a plugin.

Usage:
  shiki-install setup --component region|browser|all --user <ordinary-user> [flags]
  shiki-install maintenance --component region|browser|all --user <ordinary-user> [--update --yes]

Without --yes these commands print a read-only plan and detected installed versions.
No public network, package installation, APT source/key changes, linger changes or
helper authorization occur while planning. Review the entire plan before --yes.

Setup requires a local extracted --bundle, its matching --archive and --signature,
a fixed --version and an independently provisioned root-owned --public-key. A pin
computed from the bundle is not root trust. Production binaries install only at
/usr/lib/shikigami/shiki-region-helper and /usr/lib/shikigami/shiki-browser-runtime.
Neither is run as root from a user's plugin store.

--stage-only --root-prefix <private-fixture> stages inert signed support artifacts;
it never executes a helper, runs APT/systemd or authorizes an alternate helper root.
Maintenance inspects by default; --update --yes explicitly upgrades allowlisted
stable dependencies/official Chrome. Support binary updates use setup with a new
signed release. Packages are not automatically rolled back. Running Chrome is not
restarted. Browser profiles and region rollback evidence are preserved by uninstall.
Uninstall revokes authorization and quiesces the trusted installed root helper;
pending/conflicted DNS recovery blocks removal and preserves its guard/helper/policy.
Only a clean disabled status permits removing that privileged service and code.

Supported server platforms: Ubuntu 24.04 / Debian 12, amd64 / arm64, systemd.
Unattended boot restore can explicitly enroll a systemd encrypted credential:
  --boot-unlock --keyring-mode dedicated|existing-login
Create a new absent dedicated keyring only with --provision-keyring. Existing
keyrings are never reset. Read the password in a hidden terminal, or select
--keyring-secret-stdin / --keyring-secret-file; password values are never flags,
environment variables or logs. --reuse-keyring-credential explicitly reuses an
earlier protected encrypted credential. Credential maintenance without --update
does not refresh APT or contact a public network.

Encrypted source: /etc/shikigami/credentials/browser-keyring-<uid>.cred
Unlock service: shikigami-browser-keyring-<uid>.service (ordinary UID)
Host-key encryption does not protect against root or a whole-disk copy together
with /var/lib/systemd/credential.secret; no TPM or plaintext fallback is implied.
Enrollment requires explicitly stopped Chrome. Uninstall removes the unlock
service/policy but preserves encrypted credential, keyring and profile recovery data.

Browser setup/updates require this mandatory host-wide official Chrome policy:
  /etc/opt/chrome/policies/managed/shikigami-browser.json
  PasswordManagerEnabled=false, SyncDisabled=true, BrowserSignin=0
It affects ALL official Chrome users/profiles on the host; existing Chrome may
refresh policy without restarting. Conflicting/unverifiable policies are rejected,
not overwritten. Only an unchanged policy created by this installer is removed on
uninstall, after managed browsers have stopped.
`

func executeExtension(ctx context.Context, operation string, args []string, out, errOut io.Writer) (int, error) {
	flags := flag.NewFlagSet("shiki-install "+operation, flag.ContinueOnError)
	flags.SetOutput(errOut)
	flags.Usage = func() {}
	var options install.ExtensionOptions
	var secretStdin bool
	var secretFile string
	flags.StringVar(&options.Component, "component", "all", "region, browser or all; fixed dependency/source allowlists only")
	flags.StringVar(&options.User, "user", "", "existing ordinary daemon account")
	flags.BoolVar(&options.Yes, "yes", false, "authorize the complete printed plan; never inferred from non-interactive input")
	flags.BoolVar(&options.BootUnlock, "boot-unlock", false, "explicit administrator enrollment of systemd encrypted credential and unattended keyring unlock service")
	flags.StringVar(&options.KeyringMode, "keyring-mode", "", "explicit dedicated or existing-login; changing configured mode is refused")
	flags.BoolVar(&options.ProvisionKeyring, "provision-keyring", false, "create only a new absent dedicated encrypted keyring; never reset existing user keyrings")
	flags.BoolVar(&options.ReuseKeyringCredential, "reuse-keyring-credential", false, "explicitly reuse a previously enrolled root-protected encrypted credential")
	flags.BoolVar(&secretStdin, "keyring-secret-stdin", false, "read password only from explicit private non-terminal stdin after plan confirmation")
	flags.StringVar(&secretFile, "keyring-secret-file", "", "read password from an explicit owned 0600 regular single-link file; never argv or logs")
	if operation == "setup" {
		flags.StringVar(&options.Bundle, "bundle", "", "absolute extracted signed local bundle")
		flags.StringVar(&options.Version, "version", "", "fixed matching signed release version")
		flags.StringVar(&options.Archive, "archive", "", "canonical local signed release archive")
		flags.StringVar(&options.Signature, "signature", "", "detached Ed25519 release signature")
		flags.StringVar(&options.PublicKey, "public-key", "/etc/shikigami/release-signing.pub", "independently trusted root-owned Ed25519 key; never a key fetched with the bundle")
		flags.BoolVar(&options.StageOnly, "stage-only", false, "stage inert fixtures only; no host packages, services or grants")
		flags.StringVar(&options.RootPrefix, "root-prefix", "", "private inert fixture root; requires --stage-only, never passed to production helper")
	} else {
		flags.BoolVar(&options.Update, "update", false, "explicitly refresh and upgrade allowlisted stable dependencies; no running Chrome restart")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if _, err = io.WriteString(out, extensionInstallerHelp+"\nFlags:\n"); err != nil {
				return 1, err
			}
			flags.SetOutput(out)
			flags.PrintDefaults()
			return 0, nil
		}
		return 2, err
	}
	if flags.NArg() != 0 {
		return 2, errors.New("unexpected positional arguments; arbitrary packages, sources and commands are not accepted")
	}
	if (secretStdin || secretFile != "") && (!options.BootUnlock || options.ReuseKeyringCredential) {
		return 2, errors.New("secret input requires --boot-unlock and cannot be combined with --reuse-keyring-credential")
	}
	if secretStdin && secretFile != "" {
		return 2, errors.New("select one secret input source")
	}
	if options.BootUnlock && !options.ReuseKeyringCredential && !options.StageOnly {
		options.ReadKeyringSecret = keyringSecretReader(options.User, secretStdin, secretFile, options.ProvisionKeyring)
	}
	plan, err := install.ExtensionPlatformPlan(ctx, operation, options)
	if err != nil {
		return 2, err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(plan); err != nil {
		return 1, err
	}
	if !options.Yes {
		return 0, nil
	}
	if err = install.ApplyExtensionPlan(ctx, operation, options); err != nil {
		return 1, err
	}
	state := "applied"
	if options.StageOnly {
		state = "staged_inert"
	} else if operation == "maintenance" && !options.Update && !options.BootUnlock {
		state = "inspected"
	}
	_, err = fmt.Fprintf(out, "{\"state\":%q,\"chrome_restarted\":false,\"packages_auto_rolled_back\":false}\n", state)
	return 0, err
}
