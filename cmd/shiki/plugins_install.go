package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/spf13/cobra"
)

const (
	pluginMaxArchiveBytes   = int64(256 << 20)
	pluginMaxManifest       = int64(64 << 10)
	pluginMaxBinary         = int64(256 << 20)
	pluginMaxExpandedBytes  = pluginMaxBinary + pluginMaxManifest + (1 << 20)
	pluginMaxArchiveMembers = 16
	pluginHTTPTimeout       = 5 * time.Minute
	pluginRuntimeTimeout    = 30 * time.Second
	pluginInstallMetaFormat = 1
)

type pluginInstallMeta struct {
	Format       int    `json:"format"`
	SourceKind   string `json:"source_kind"`
	SourceURL    string `json:"source_url"`
	SHA256       string `json:"sha256,omitempty"`
	MarketURL    string `json:"market_url,omitempty"`
	SignatureURL string `json:"signature_url,omitempty"`
	KeyID        string `json:"key_id,omitempty"`
	KeySHA256    string `json:"key_sha256,omitempty"`
	InstalledAt  string `json:"installed_at"`
}

func validPluginID(id string) bool { return client.ValidatePluginMarketID(id) == nil }

func protectedPluginID(id string) bool { return id == "sync" || id == "port" }

func (a *app) pluginInstallCommands() []*cobra.Command {
	return []*cobra.Command{a.pluginInstallCmd(), a.pluginUpdateCmd(), a.pluginUninstallCmd(), a.pluginTrustCmd(), a.pluginMarketCmd()}
}

func (a *app) pluginInstallCmd() *cobra.Command {
	var sha256Flag, marketURL, versionFlag string
	cmd := &cobra.Command{
		Use: "install <url-or-path-or-plugin-id>", Short: "Install a third-party plugin",
		Long: `Install a plugin archive after validating its manifest and binary.

A URL or local path is a manual install and may include --sha256. A plugin ID
uses a configured signed marketplace; signed index and archive metadata are
mandatory for that path. Plugins run with this user's permissions, not in a
sandbox. Installation leaves the plugin disabled; enable it explicitly.`,
		Args: cobra.ExactArgs(1), PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			if isURL(args[0]) || strings.ContainsAny(args[0], `/\\.`) {
				if marketURL != "" || versionFlag != "" {
					return errors.New("--market and --version apply only to marketplace plugin IDs")
				}
				return a.doPluginInstall(cmd.Context(), args[0], sha256Flag)
			}
			if sha256Flag != "" {
				return errors.New("--sha256 applies only to manual archive installs")
			}
			return a.installFromMarket(cmd.Context(), args[0], versionFlag, marketURL)
		},
	}
	cmd.Flags().StringVar(&sha256Flag, "sha256", "", "Expected SHA-256 hex digest for a manual archive install")
	cmd.Flags().StringVar(&marketURL, "market", "", "Signed marketplace index URL")
	cmd.Flags().StringVar(&versionFlag, "version", "", "Exact marketplace plugin version")
	return cmd
}

func (a *app) withPluginMutation(ctx context.Context, run func(*client.PluginInstallSession, *client.PluginInstallStore) error) error {
	if a.timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	session, err := client.OpenPluginInstallSession(ctx, a.configPath, a.timeout)
	if err != nil {
		return err
	}
	defer session.Close()
	store, err := client.OpenPluginInstallStore(ctx, session.Directory)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := a.recoverPluginInstall(ctx, session, store); err != nil {
		return fmt.Errorf("plugin transaction recovery: %w", err)
	}
	return run(session, store)
}

func (a *app) doPluginInstall(ctx context.Context, source, expectedSHA256 string) error {
	return a.withPluginMutation(ctx, func(session *client.PluginInstallSession, store *client.PluginInstallStore) error {
		file, cleanup, err := fetchPluginSource(ctx, source, "archive.tar.gz")
		if err != nil {
			return err
		}
		defer cleanup()
		if expectedSHA256 != "" {
			if err := verifySHA256(ctx, file, expectedSHA256); err != nil {
				return err
			}
			fmt.Fprintln(a.output, "SHA-256 verified")
		} else {
			fmt.Fprintf(a.output, "Warning: manual archive %q has no publisher authentication or --sha256 pin.\n", source)
		}
		meta := pluginInstallMeta{Format: pluginInstallMetaFormat, SourceKind: "manual", SourceURL: source, SHA256: strings.ToLower(strings.TrimSpace(expectedSHA256))}
		return a.installPluginFile(ctx, session, store, file, meta, nil, false)
	})
}

func (a *app) installPluginFile(ctx context.Context, session *client.PluginInstallSession, store *client.PluginInstallStore, file *os.File, meta pluginInstallMeta, expected *client.PluginMarketEntry, update bool) error {
	stage, err := os.MkdirTemp(store.Base, ".plugin-stage-")
	if err != nil {
		return err
	}
	retained := false
	defer func() {
		if !retained {
			_ = os.RemoveAll(stage)
		}
	}()
	manifest, err := extractPluginArchive(ctx, file, stage)
	if err != nil {
		return fmt.Errorf("extracting plugin archive: %w", err)
	}
	if protectedPluginID(manifest.ID) {
		return fmt.Errorf("cannot overwrite built-in plugin %q", manifest.ID)
	}
	if expected != nil && (manifest.ID != expected.ID || manifest.Version != expected.Version || expected.OS != runtime.GOOS || expected.Arch != runtime.GOARCH) {
		return errors.New("extracted manifest does not match authenticated marketplace ID/version/platform")
	}
	dest := filepath.Join(store.Base, manifest.ID)
	_, statErr := os.Lstat(dest)
	if !update && statErr == nil {
		return fmt.Errorf("plugin %q is already installed; use `shiki plugins update %s`", manifest.ID, manifest.ID)
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if update && errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("plugin %q is not installed", manifest.ID)
	}
	if update && session.Client == nil {
		return errors.New("plugin update requires a running agent to stop and restore the plugin; start it explicitly")
	}
	meta.InstalledAt = time.Now().UTC().Format(time.RFC3339)
	if err := validateInstallMeta(meta); err != nil {
		return err
	}
	if err := writePluginJSON(stage, ".install-meta.json", meta); err != nil {
		return err
	}
	manifestDigest, err := pluginFileDigest(ctx, filepath.Join(stage, "manifest.json"), pluginMaxManifest)
	if err != nil {
		return err
	}
	binaryDigest, err := pluginFileDigest(ctx, filepath.Join(stage, manifest.Binary), pluginMaxBinary)
	if err != nil {
		return err
	}
	receipt := struct {
		ManifestSHA256 string `json:"manifest_sha256"`
		BinarySHA256   string `json:"binary_sha256"`
	}{manifestDigest, binaryDigest}
	if err := writePluginJSON(stage, ".artifact-integrity.json", receipt); err != nil {
		return err
	}
	if err := syncPluginDirectory(stage); err != nil {
		return err
	}
	if err := store.Sync(); err != nil {
		return err
	}
	var status *pb.PluginStatus
	if session.Client != nil {
		status, err = installerPluginStatus(ctx, session.Client, manifest.ID)
		if err != nil {
			return err
		}
	}
	if update && status == nil {
		return errors.New("installed plugin is not admitted by the running agent; no files changed")
	}
	operation, enabled, revision := "install", false, ""
	if update {
		operation, enabled = "update", status.Enabled
	}
	if status != nil {
		revision = status.Revision
	}
	tx, err := store.Begin(manifest.ID, operation, filepath.Base(stage), enabled, revision)
	if err != nil {
		return err
	}
	retained = true
	if status != nil {
		if err := a.stopInstalledPlugin(ctx, session.Client, status, store, tx); err != nil {
			return fmt.Errorf("plugin stop not confirmed; package unchanged and recovery intent retained: %w", err)
		}
	}
	if err := store.Publish(tx); err != nil {
		recoveryErr := a.recoverPluginInstall(ctx, session, store)
		return errors.Join(err, recoveryErr)
	}
	if err := a.completePluginInstall(ctx, session, store, tx); err != nil {
		return err
	}
	fmt.Fprintf(a.output, "Plugin %q (%s) installed to %s\n", manifest.ID, manifest.Version, dest)
	if enabled {
		fmt.Fprintln(a.output, "The plugin's previous enabled state has been restored and is ready.")
	} else {
		fmt.Fprintf(a.output, "Run `shiki plugins enable %s --side local` to activate it.\n", manifest.ID)
	}
	return nil
}

func (a *app) installFromMarket(ctx context.Context, id, version, marketURL string) error {
	if !validPluginID(id) || protectedPluginID(id) {
		return fmt.Errorf("invalid or built-in marketplace plugin id %q", id)
	}
	if version != "" {
		if err := client.ValidatePluginVersion(version); err != nil {
			return err
		}
	}
	return a.withPluginMutation(ctx, func(session *client.PluginInstallSession, store *client.PluginInstallStore) error {
		indexURL, err := a.selectMarketURL(marketURL)
		if err != nil {
			return err
		}
		trust, err := a.loadPluginTrustStore()
		if err != nil {
			return err
		}
		index, err := fetchPluginIndex(ctx, indexURL, trust)
		if err != nil {
			return err
		}
		entry, err := choosePluginEntry(index, id, version)
		if err != nil {
			return err
		}
		return a.installMarketEntry(ctx, session, store, indexURL, entry, trust, false)
	})
}

func (a *app) pluginUpdateCmd() *cobra.Command {
	var marketURL, version string
	cmd := &cobra.Command{
		Use: "update [plugin-id]", Short: "Explicitly update third-party plugins from signed marketplaces",
		Args: cobra.MaximumNArgs(1), PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			if version != "" {
				if err := client.ValidatePluginVersion(version); err != nil {
					return err
				}
			}
			if len(args) == 1 && (!validPluginID(args[0]) || protectedPluginID(args[0])) {
				return errors.New("invalid or built-in plugin id")
			}
			return a.withPluginMutation(cmd.Context(), func(session *client.PluginInstallSession, store *client.PluginInstallStore) error {
				ids := args
				if len(ids) == 0 {
					var err error
					ids, err = listInstalledThirdPartyPlugins(store.Base)
					if err != nil {
						return err
					}
				}
				if len(ids) == 0 {
					fmt.Fprintln(a.output, "No third-party plugins found.")
					return nil
				}
				var errs []error
				for _, id := range ids {
					if err := a.updateOnePlugin(cmd.Context(), session, store, id, version, marketURL); err != nil {
						if errors.Is(err, errPluginNotMarket) && len(args) == 0 {
							fmt.Fprintf(a.output, "Plugin %q was installed manually; skipping marketplace update.\n", id)
							continue
						}
						errs = append(errs, fmt.Errorf("%s: %w", id, err))
						if pending, recoveryErr := store.Pending(); recoveryErr != nil || pending != nil {
							return errors.Join(errors.Join(errs...), recoveryErr)
						}
					}
				}
				return errors.Join(errs...)
			})
		},
	}
	cmd.Flags().StringVar(&marketURL, "market", "", "Explicit marketplace URL override (publisher key must remain unchanged)")
	cmd.Flags().StringVar(&version, "version", "", "Exact marketplace plugin version")
	return cmd
}

func (a *app) pluginUninstallCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "uninstall <plugin-id>", Short: "Remove an installed third-party plugin", Args: cobra.ExactArgs(1),
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !validPluginID(id) || protectedPluginID(id) {
				return fmt.Errorf("invalid or built-in plugin id %q", id)
			}
			return a.withPluginMutation(cmd.Context(), func(session *client.PluginInstallSession, store *client.PluginInstallStore) error {
				dir := filepath.Join(store.Base, id)
				if _, err := os.Lstat(dir); err != nil {
					return fmt.Errorf("plugin %q is not installed: %w", id, err)
				}
				if session.Client == nil {
					return errors.New("plugin uninstall requires a running agent to persist disabled state and confirm process exit; start it explicitly")
				}
				if err := a.confirm(cmd.Context(), "Remove plugin "+id+" from "+dir, yes); err != nil {
					return err
				}
				status, err := installerPluginStatus(cmd.Context(), session.Client, id)
				if err != nil {
					return err
				}
				if status == nil {
					return errors.New("installed plugin is not admitted by the running agent; stop/repair it before uninstall")
				}
				tx, err := store.Begin(id, "uninstall", "", status.Enabled, status.Revision)
				if err != nil {
					return err
				}
				if err := a.stopInstalledPlugin(cmd.Context(), session.Client, status, store, tx); err != nil {
					return fmt.Errorf("plugin stop not confirmed; no plugin files removed: %w", err)
				}
				if err := store.Publish(tx); err != nil {
					return errors.Join(err, a.recoverPluginInstall(cmd.Context(), session, store))
				}
				if err := a.completePluginInstall(cmd.Context(), session, store, tx); err != nil {
					return err
				}
				fmt.Fprintf(a.output, "Plugin %q removed. Configuration and state data are not affected.\n", id)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func installerPluginStatus(ctx context.Context, c *client.Client, id string) (*pb.PluginStatus, error) {
	list, err := c.Agent().ListPlugins(ctx, &apb.PluginTarget{Side: apb.PluginTarget_LOCAL})
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

func (a *app) manageInstallRuntime(ctx context.Context, c *client.Client, store *client.PluginInstallStore, tx *client.PluginInstallTransaction, action pb.ManagePluginRequest_Action, status *pb.PluginStatus) (*pb.PluginStatus, error) {
	name := "disable"
	if action == pb.ManagePluginRequest_ENABLE {
		name = "enable"
	}
	if tx.RuntimeAction == "" {
		if status == nil || status.Revision != tx.RuntimeRevision {
			return nil, errors.New("plugin runtime revision changed; retry to recover using the latest desired state")
		}
		if err := store.PrepareRuntimeAction(tx, name); err != nil {
			return nil, err
		}
	} else if tx.RuntimeAction != name {
		return nil, errors.New("another journaled plugin runtime action must finish before proceeding")
	}
	wait, cancel := context.WithTimeout(ctx, pluginRuntimeTimeout)
	defer cancel()
	target := &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}
	accepted, err := c.Agent().ManagePlugin(wait, &apb.ManagePluginRequest{Target: target, Request: &pb.ManagePluginRequest{PluginId: tx.ID, Action: action, ExpectedRevision: tx.RuntimeExpectedRevision, RequestId: tx.RuntimeRequestID}})
	if err != nil {
		// These host rejections occur before mutation (and after request replay).
		// Transport/storage failures remain pending because acceptance is unknown.
		switch connect.CodeOf(err) {
		case connect.CodeInvalidArgument, connect.CodeFailedPrecondition, connect.CodeNotFound, connect.CodeAborted:
			return nil, errors.Join(err, store.ObserveRuntimeAction(tx, tx.RuntimeRevision, true))
		}
		return nil, err
	}
	if err := store.ObserveRuntimeAction(tx, accepted.Revision, false); err != nil {
		return accepted, err
	}
	final, waitErr := waitForPlugin(wait, accepted, func(ctx context.Context) (*pb.PluginStatus, error) { return pluginStatus(ctx, c, target, tx.ID) })
	if final != nil && final.Revision == accepted.Revision && (waitErr == nil || final.State == "failed" || final.State == "incompatible") {
		if err := store.ObserveRuntimeAction(tx, final.Revision, true); err != nil {
			return final, errors.Join(waitErr, err)
		}
	}
	return final, waitErr
}

func (a *app) stopInstalledPlugin(ctx context.Context, c *client.Client, status *pb.PluginStatus, store *client.PluginInstallStore, tx *client.PluginInstallTransaction) error {
	if status.Revision != tx.RuntimeRevision {
		return errors.New("plugin runtime revision changed; refusing to overwrite a newer desired state")
	}
	if !status.Enabled && status.State == "stopped" && tx.RuntimeAction == "" {
		return nil
	}
	final, err := a.manageInstallRuntime(ctx, c, store, tx, pb.ManagePluginRequest_DISABLE, status)
	if err != nil {
		return err
	}
	if final.Enabled || final.State != "stopped" {
		return errors.New("plugin process exit was not confirmed")
	}
	return nil
}

func (a *app) enableInstalledPlugin(ctx context.Context, c *client.Client, id, version string, store *client.PluginInstallStore, tx *client.PluginInstallTransaction) error {
	if c == nil {
		return errors.New("runtime restoration requires a running agent; start it explicitly and retry the plugin operation")
	}
	status, err := pluginStatus(ctx, c, &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}, id)
	if err != nil {
		return err
	}
	if status.Revision != tx.RuntimeRevision {
		return errors.New("plugin runtime revision changed; refusing to restore an obsolete enabled state")
	}
	if !status.Enabled || status.State != "ready" || tx.RuntimeAction != "" {
		status, err = a.manageInstallRuntime(ctx, c, store, tx, pb.ManagePluginRequest_ENABLE, status)
		if err != nil {
			return err
		}
	}
	if !status.Enabled || status.State != "ready" || status.BuildVersion != version {
		return fmt.Errorf("plugin activation did not confirm installed version %q (runtime %q)", version, status.BuildVersion)
	}
	return nil
}

func (a *app) recoverPluginInstall(ctx context.Context, session *client.PluginInstallSession, store *client.PluginInstallStore) error {
	tx, err := store.Pending()
	if err != nil || tx == nil {
		return err
	}
	if session.Client == nil && (tx.Operation != "install" || tx.RuntimeAction != "") {
		return errors.New("an interrupted plugin mutation requires a running agent for safe recovery; start it explicitly and retry")
	}
	if session.Client != nil {
		status, err := installerPluginStatus(ctx, session.Client, tx.ID)
		if err != nil {
			return err
		}
		if tx.RuntimeAction != "" {
			action := pb.ManagePluginRequest_DISABLE
			if tx.RuntimeAction == "enable" {
				action = pb.ManagePluginRequest_ENABLE
			}
			if _, replayErr := a.manageInstallRuntime(ctx, session.Client, store, tx, action, status); replayErr != nil {
				status, err = installerPluginStatus(ctx, session.Client, tx.ID)
				if err != nil || status == nil || status.Revision == tx.RuntimeRevision {
					if action == pb.ManagePluginRequest_ENABLE && err == nil && status != nil && tx.RuntimeAction == "" {
						return a.rollbackPluginInstall(ctx, session, store, tx, replayErr)
					}
					return errors.Join(replayErr, err)
				}
			}
			status, err = installerPluginStatus(ctx, session.Client, tx.ID)
			if err != nil {
				return err
			}
		}
		if status != nil && status.Revision != tx.RuntimeRevision {
			if tx.Operation == "install" && tx.Phase == "prepared" && status.Enabled {
				return errors.New("plugin was enabled while its first installation was incomplete; disable it explicitly before retrying recovery")
			}
			if err := store.AdoptRuntimeState(tx, status.Revision, status.Enabled); err != nil {
				return err
			}
		} else if status == nil && tx.RuntimeRevision != "" {
			return errors.New("journaled plugin runtime state is unavailable; previous package retained")
		}
		mustStop := tx.Phase == "prepared"
		if tx.Phase == "committed" && tx.Operation == "update" {
			if _, err := os.Lstat(filepath.Join(store.Base, tx.ID)); errors.Is(err, os.ErrNotExist) {
				mustStop = true
			}
		}
		if mustStop && status != nil {
			if err := a.stopInstalledPlugin(ctx, session.Client, status, store, tx); err != nil {
				return err
			}
		}
	}
	if err := store.RecoverPublication(tx); err != nil {
		return err
	}
	return a.completePluginInstall(ctx, session, store, tx)
}

func (a *app) completePluginInstall(ctx context.Context, session *client.PluginInstallSession, store *client.PluginInstallStore, tx *client.PluginInstallTransaction) error {
	var version string
	packageRequired := tx.Phase == "committed" && tx.Operation != "uninstall" || tx.Phase == "rolled-back" && tx.Operation != "install" && tx.RestoreEnabled
	if packageRequired {
		var err error
		version, err = validateInstalledArtifact(ctx, tx.ID, store.Base)
		if err != nil {
			return a.rollbackPluginInstall(ctx, session, store, tx, err)
		}
	}
	if tx.RestoreEnabled && !(tx.Operation == "uninstall" && tx.Phase == "committed") {
		if err := a.enableInstalledPlugin(ctx, session.Client, tx.ID, version, store, tx); err != nil {
			return a.rollbackPluginInstall(ctx, session, store, tx, fmt.Errorf("plugin activation failed: %w", err))
		}
	} else if session.Client != nil {
		// Refresh discovery without enabling new installations or disabled updates.
		status, err := installerPluginStatus(ctx, session.Client, tx.ID)
		if err != nil {
			return err
		}
		if status != nil && (status.Enabled || status.State != "stopped") {
			if err := a.stopInstalledPlugin(ctx, session.Client, status, store, tx); err != nil {
				return err
			}
		}
	}
	return store.Finish(tx)
}

func (a *app) rollbackPluginInstall(ctx context.Context, session *client.PluginInstallSession, store *client.PluginInstallStore, tx *client.PluginInstallTransaction, cause error) error {
	if tx.Operation != "update" || tx.Phase != "committed" || session.Client == nil {
		return cause
	}
	status, err := installerPluginStatus(ctx, session.Client, tx.ID)
	if err != nil || status == nil {
		return errors.Join(cause, err, errors.New("previous package retained; runtime state could not be confirmed for rollback"))
	}
	if err := a.stopInstalledPlugin(ctx, session.Client, status, store, tx); err != nil {
		return errors.Join(cause, err)
	}
	if err := store.Rollback(tx); err != nil {
		return errors.Join(cause, err)
	}
	if tx.RestoreEnabled {
		oldVersion, err := validateInstalledArtifact(ctx, tx.ID, store.Base)
		if err != nil {
			return errors.Join(cause, err)
		}
		if err := a.enableInstalledPlugin(ctx, session.Client, tx.ID, oldVersion, store, tx); err != nil {
			return errors.Join(cause, fmt.Errorf("previous package restored but activation is pending: %w", err))
		}
	}
	return errors.Join(cause, store.Finish(tx))
}

// Count physical tar headers before archive/tar can silently consume PAX/GNU
// extension records. The supported package format needs only regular files and
// optional directories; all other record types are rejected before their body.
type pluginTarBudgetReader struct {
	reader               io.Reader
	header               [512]byte
	pending              []byte
	body                 int64
	members, memberLimit int
	totalLimit           int64
	trailing             bool
}

func (r *pluginTarBudgetReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) != 0 {
		count := copy(p, r.pending)
		r.pending = r.pending[count:]
		return count, nil
	}
	if r.trailing {
		return r.reader.Read(p)
	}
	if r.body > 0 {
		if int64(len(p)) > r.body {
			p = p[:int(r.body)]
		}
		count, err := r.reader.Read(p)
		r.body -= int64(count)
		return count, err
	}
	if _, err := io.ReadFull(r.reader, r.header[:]); err != nil {
		return 0, err
	}
	zero := true
	for _, b := range r.header {
		if b != 0 {
			zero = false
			break
		}
	}
	if zero {
		r.trailing = true
	} else {
		r.members++
		if r.members > r.memberLimit {
			return 0, errors.New("archive exceeds member count limit")
		}
		kind := r.header[156]
		if kind != tar.TypeReg && kind != tar.TypeRegA && kind != tar.TypeDir {
			return 0, errors.New("archive contains unsupported physical tar metadata/member")
		}
		var size int64
		digits, ended := false, false
		for _, b := range r.header[124:136] {
			if b == 0 || b == ' ' {
				if digits {
					ended = true
				}
				continue
			}
			if b < '0' || b > '7' || ended {
				return 0, errors.New("archive contains unsupported tar size encoding")
			}
			digits = true
			size = size*8 + int64(b-'0')
			if size > r.totalLimit {
				return 0, errors.New("archive exceeds total decompression budget")
			}
		}
		if kind == tar.TypeDir && size != 0 {
			return 0, errors.New("archive directory contains unexpected data")
		}
		r.body = (size + 511) / 512 * 512
	}
	count := copy(p, r.header[:])
	r.pending = r.header[count:]
	return count, nil
}

type pluginContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r pluginContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func extractPluginArchive(ctx context.Context, file *os.File, stagingDir string) (*sdk.PluginManifest, error) {
	return extractPluginArchiveLimited(ctx, file, stagingDir, pluginMaxExpandedBytes, pluginMaxArchiveMembers)
}

func extractPluginArchiveLimited(ctx context.Context, file *os.File, stagingDir string, totalLimit int64, memberLimit int) (*sdk.PluginManifest, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	compressed := bufio.NewReader(pluginContextReader{ctx: ctx, reader: file})
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("invalid gzip archive: %w", err)
	}
	defer gz.Close()
	gz.Multistream(false)
	expanded := &io.LimitedReader{R: pluginContextReader{ctx: ctx, reader: gz}, N: totalLimit + 1}
	physical := &pluginTarBudgetReader{reader: expanded, memberLimit: memberLimit, totalLimit: totalLimit}
	tr := tar.NewReader(physical)
	var manifest *sdk.PluginManifest
	var binaryName, root string
	rootSet := false
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if expanded.N <= 0 {
			return nil, errors.New("archive exceeds total decompression budget")
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading archive: %w", err)
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if hdr.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
			if name == "" || name == "." {
				continue
			}
		}
		if name == "" || strings.ContainsAny(name, `\\:`) || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("unsafe archive path %q", hdr.Name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate archive member %q", hdr.Name)
		}
		seen[name] = true
		if len(hdr.PAXRecords) != 0 || len(hdr.Xattrs) != 0 {
			return nil, errors.New("archive contains unsupported extended metadata")
		}
		parts := strings.Split(name, "/")
		if hdr.Typeflag == tar.TypeDir {
			if len(parts) != 1 || (rootSet && root != name) {
				return nil, errors.New("archive contains unexpected directories")
			}
			root, rootSet = name, true
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("archive contains unsupported member %q", hdr.Name)
		}
		prefix, base := "", name
		if len(parts) == 2 {
			prefix, base = parts[0], parts[1]
		} else if len(parts) != 1 {
			return nil, errors.New("archive must be flat or use one common top-level directory")
		}
		if rootSet && root != prefix {
			return nil, errors.New("archive contains ambiguous package roots")
		}
		root, rootSet = prefix, true
		switch {
		case base == "manifest.json":
			if manifest != nil || hdr.Size < 1 || hdr.Size > pluginMaxManifest {
				return nil, errors.New("duplicate or oversized manifest.json")
			}
			data, err := io.ReadAll(io.LimitReader(tr, pluginMaxManifest+1))
			if err != nil {
				return nil, err
			}
			var parsed sdk.PluginManifest
			if err := client.DecodeStrictJSON(data, &parsed); err != nil {
				return nil, fmt.Errorf("invalid manifest.json: %w", err)
			}
			if err := validateInstalledManifest(&parsed); err != nil {
				return nil, err
			}
			if err := writePluginBytes(filepath.Join(stagingDir, base), data, 0600); err != nil {
				return nil, err
			}
			manifest = &parsed
		case strings.HasPrefix(base, "shiki-plugin-") && validPluginID(strings.TrimPrefix(base, "shiki-plugin-")):
			if binaryName != "" || hdr.Size < 1 || hdr.Size > pluginMaxBinary {
				return nil, errors.New("archive contains multiple, empty or oversized plugin binaries")
			}
			if err := writeExtractedFile(tr, filepath.Join(stagingDir, base), pluginMaxBinary); err != nil {
				return nil, err
			}
			binaryName = base
		default:
			return nil, fmt.Errorf("unexpected archive member %q", hdr.Name)
		}
	}
	// Consume the gzip trailer, rejecting non-padding tar data, concatenated
	// gzip streams and expansion hidden after tar's end-of-archive marker.
	var padding [32 << 10]byte
	for {
		n, err := expanded.Read(padding[:])
		if expanded.N <= 0 {
			return nil, errors.New("archive exceeds total decompression budget")
		}
		for _, b := range padding[:n] {
			if b != 0 {
				return nil, errors.New("archive contains trailing non-padding content")
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if _, err := compressed.Peek(1); !errors.Is(err, io.EOF) {
		return nil, errors.New("archive contains a concatenated gzip stream or trailing bytes")
	}
	if manifest == nil || binaryName != manifest.Binary {
		return nil, errors.New("archive must contain exactly manifest.json and its declared plugin binary")
	}
	return manifest, nil
}

func validateInstalledManifest(manifest *sdk.PluginManifest) error {
	if err := sdk.ValidateManifest(manifest); err != nil {
		return fmt.Errorf("invalid plugin manifest: %w", err)
	}
	return client.ValidatePluginVersion(manifest.Version)
}

func writeExtractedFile(reader io.Reader, dest string, maxBytes int64) (err error) {
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	count, err := io.Copy(file, io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return err
	}
	if count > maxBytes {
		return errors.New("file exceeds size limit")
	}
	return file.Sync()
}

func pluginHTTPClient() *http.Client {
	return &http.Client{Timeout: pluginHTTPTimeout, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if !isHTTPSPluginURL(request.URL.String()) {
			return errors.New("plugin downloads require HTTPS on every redirect")
		}
		if len(via) >= 10 {
			return errors.New("too many plugin download redirects")
		}
		return nil
	}}
}

func fetchPluginSource(ctx context.Context, source, filename string) (*os.File, func(), error) {
	dir, err := os.MkdirTemp("", "shiki-plugin-download-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if filepath.Base(filename) != filename || strings.ContainsAny(filename, `/\\`) {
		cleanup()
		return nil, nil, errors.New("invalid private download filename")
	}
	out, err := os.OpenFile(filepath.Join(dir, filename), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeAndClean := func() { _ = out.Close(); cleanup() }
	var input io.ReadCloser
	if isURL(source) {
		if !isHTTPSPluginURL(source) {
			closeAndClean()
			return nil, nil, errors.New("plugin archive URL must use HTTPS")
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			closeAndClean()
			return nil, nil, err
		}
		response, err := pluginHTTPClient().Do(request)
		if err != nil {
			closeAndClean()
			return nil, nil, fmt.Errorf("downloading plugin: %w", err)
		}
		if response.StatusCode != http.StatusOK || response.ContentLength > pluginMaxArchiveBytes {
			response.Body.Close()
			closeAndClean()
			return nil, nil, errors.New("plugin download returned a non-200 response or exceeds size limit")
		}
		input = response.Body
	} else {
		file, err := os.OpenFile(source, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			closeAndClean()
			return nil, nil, fmt.Errorf("opening plugin archive: %w", err)
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > pluginMaxArchiveBytes {
			file.Close()
			closeAndClean()
			return nil, nil, errors.New("plugin archive must be a bounded regular file")
		}
		input = file
	}
	count, copyErr := io.Copy(out, io.LimitReader(pluginContextReader{ctx: ctx, reader: input}, pluginMaxArchiveBytes+1))
	closeErr := input.Close()
	if copyErr != nil || closeErr != nil || count > pluginMaxArchiveBytes {
		closeAndClean()
		return nil, nil, errors.Join(copyErr, closeErr, errors.New("plugin archive download did not complete within its size limit"))
	}
	if err := out.Sync(); err != nil {
		closeAndClean()
		return nil, nil, err
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		closeAndClean()
		return nil, nil, err
	}
	return out, closeAndClean, nil
}

func isURL(source string) bool {
	parsed, err := url.Parse(source)
	return err == nil && parsed.Scheme != ""
}

func verifySHA256(ctx context.Context, file *os.File, expected string) error {
	clean := strings.ToLower(strings.TrimSpace(expected))
	if !validPluginDigest(clean) {
		return errors.New("--sha256 must be a 64-character hexadecimal digest")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(pluginContextReader{ctx: ctx, reader: file}, pluginMaxArchiveBytes+1))
	if err != nil {
		return err
	}
	if count > pluginMaxArchiveBytes || hex.EncodeToString(hash.Sum(nil)) != clean {
		return errors.New("plugin archive SHA-256 mismatch")
	}
	_, err = file.Seek(0, io.SeekStart)
	return err
}

func validPluginDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

func pluginFileDigest(ctx context.Context, name string, limit int64) (string, error) {
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return "", errors.New("plugin artifact is not a bounded regular file")
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(pluginContextReader{ctx: ctx, reader: file}, limit+1))
	if err != nil {
		return "", err
	}
	if count > limit {
		return "", errors.New("plugin artifact exceeds size limit")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writePluginBytes(name string, data []byte, mode os.FileMode) (err error) {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func writePluginJSON(dir, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writePluginBytes(filepath.Join(dir, name), append(data, '\n'), 0600)
}

func syncPluginDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}
