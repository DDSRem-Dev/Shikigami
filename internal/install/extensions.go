package install

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const RegionHelperPath = "/usr/lib/shikigami/shiki-region-helper"
const BrowserRuntimePath = "/usr/lib/shikigami/shiki-browser-runtime"
const BrowserRuntimeInstallLock = "/usr/lib/shikigami/shiki-browser-runtime.install-lock"
const RegionPolicyPath = "/etc/shikigami-region-helper.json"
const RegionStatePath = "/var/lib/shikigami-region"
const RegionGuardUnit = "shikigami-region-guard.service"

// ExtensionOptions never accepts packages, sources, commands or executable paths.
// RootPrefix is exclusively an inert staging fixture, not a helper backend.
type ExtensionOptions struct {
	Component              string
	User                   string
	Bundle                 string
	Version                string
	Archive                string
	Signature              string
	PublicKey              string
	RootPrefix             string
	StageOnly              bool
	Yes                    bool
	Update                 bool
	BootUnlock             bool
	KeyringMode            string
	ProvisionKeyring       bool
	ReuseKeyringCredential bool
	ReadKeyringSecret      func(context.Context) ([]byte, error) `json:"-"`
}

type ExtensionCommand struct {
	Program string   `json:"program"`
	Args    []string `json:"args"`
	AsUser  string   `json:"as_user,omitempty"`
	Network bool     `json:"network"`
}

type ExtensionPlan struct {
	Operation                string             `json:"operation"`
	Component                string             `json:"component"`
	Platform                 string             `json:"platform"`
	Distribution             string             `json:"distribution"`
	User                     string             `json:"user"`
	UID                      int                `json:"uid"`
	StageOnly                bool               `json:"stage_only"`
	RootPrefix               string             `json:"root_prefix,omitempty"`
	BootUnlock               bool               `json:"boot_unlock,omitempty"`
	KeyringMode              string             `json:"keyring_mode,omitempty"`
	ProvisionKeyring         bool               `json:"provision_keyring,omitempty"`
	ReuseEncryptedCredential bool               `json:"reuse_encrypted_credential,omitempty"`
	Packages                 []string           `json:"packages"`
	InstalledVersions        map[string]string  `json:"installed_versions"`
	Sources                  []string           `json:"sources"`
	Paths                    []string           `json:"paths"`
	Commands                 []ExtensionCommand `json:"commands"`
	Authorization            []ExtensionCommand `json:"helper_authorization,omitempty"`
	Warnings                 []string           `json:"warnings"`
	Blocked                  []string           `json:"blocked,omitempty"`
}

func (o ExtensionOptions) includes(component string) bool {
	return o.Component == component || o.Component == "all"
}

func validateExtensionOptions(operation string, o ExtensionOptions) error {
	if operation != "setup" && operation != "maintenance" {
		return errors.New("unknown extension installation operation")
	}
	if o.Component != "region" && o.Component != "browser" && o.Component != "all" {
		return errors.New("--component must be region, browser or all")
	}
	if o.User == "" || strings.ContainsAny(o.User, "\x00\r\n") {
		return errors.New("an existing ordinary --user is required")
	}
	if o.RootPrefix != "" && (!o.StageOnly || !filepath.IsAbs(o.RootPrefix) || filepath.Clean(o.RootPrefix) == "/" || strings.ContainsAny(o.RootPrefix, "\x00\r\n")) {
		return errors.New("--root-prefix is permitted only for an inert --stage-only fixture at an absolute non-root path")
	}
	if o.Update && operation != "maintenance" {
		return errors.New("--update applies only to maintenance")
	}
	if o.StageOnly && operation != "setup" {
		return errors.New("--stage-only applies only to setup")
	}
	if o.BootUnlock && !o.includes("browser") {
		return errors.New("--boot-unlock requires --component browser or all")
	}
	if !o.BootUnlock && (o.KeyringMode != "" || o.ProvisionKeyring || o.ReuseKeyringCredential) {
		return errors.New("keyring mode, provisioning and credential reuse require --boot-unlock")
	}
	if o.BootUnlock && o.KeyringMode != "dedicated" && o.KeyringMode != "existing-login" {
		return errors.New("--boot-unlock requires explicit --keyring-mode dedicated or existing-login")
	}
	if o.ProvisionKeyring && (o.KeyringMode != "dedicated" || o.ReuseKeyringCredential) {
		return errors.New("--provision-keyring requires dedicated mode and a newly supplied secret; existing keyrings are never reset")
	}
	return nil
}

func extensionBlocked(plan *ExtensionPlan) error {
	if len(plan.Blocked) != 0 {
		return fmt.Errorf("extension plan cannot be applied: %s", strings.Join(plan.Blocked, "; "))
	}
	return nil
}

// ExtensionPlatformPlan is read-only: it neither contacts a public network nor
// modifies APT, services, authorizations, profiles or region configuration.
func ExtensionPlatformPlan(ctx context.Context, operation string, options ExtensionOptions) (*ExtensionPlan, error) {
	if err := validateExtensionOptions(operation, options); err != nil {
		return nil, err
	}
	return extensionPlatformPlan(ctx, operation, options)
}

// ApplyExtensionPlan is called only after the caller has printed the entire
// plan. --yes is explicit administrator authorization, never an implicit default.
func ApplyExtensionPlan(ctx context.Context, operation string, options ExtensionOptions) error {
	if !options.Yes {
		return errors.New("review the printed plan and pass --yes to authorize it")
	}
	return applyExtensionPlan(ctx, operation, options)
}
