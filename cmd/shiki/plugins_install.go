package main

import (
	"archive/tar"
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
	"strings"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/spf13/cobra"
)

const (
	pluginMaxArchiveBytes = int64(256 << 20)
	pluginMaxManifest     = int64(64 << 10)
	pluginMaxBinary       = int64(256 << 20)
	pluginHTTPTimeout     = 5 * time.Minute
)

type pluginInstallMeta struct {
	SourceURL    string `json:"source_url,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	MarketURL    string `json:"market_url,omitempty"`
	SignatureURL string `json:"signature_url,omitempty"`
	KeyID        string `json:"key_id,omitempty"`
	InstalledAt  string `json:"installed_at"`
}

func pluginDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".shiki", "plugins"), nil
}

func installedPluginDir(id string) (string, error) {
	if !validPluginID(id) {
		return "", fmt.Errorf("invalid plugin id %q", id)
	}
	base, err := pluginDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, id), nil
}

func validPluginID(id string) bool {
	if len(id) < 1 || len(id) > 64 || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for i := 1; i < len(id); i++ {
		if (id[i] < 'a' || id[i] > 'z') && (id[i] < '0' || id[i] > '9') && id[i] != '-' {
			return false
		}
	}
	return id[len(id)-1] != '-'
}

func (a *app) pluginInstallCommands() []*cobra.Command {
	return []*cobra.Command{
		a.pluginInstallCmd(), a.pluginUpdateCmd(), a.pluginUninstallCmd(),
		a.pluginTrustCmd(), a.pluginMarketCmd(),
	}
}

func (a *app) pluginInstallCmd() *cobra.Command {
	var sha256Flag, marketURL, versionFlag string
	cmd := &cobra.Command{
		Use:   "install <url-or-path-or-plugin-id>",
		Short: "Install a third-party plugin",
		Long: `Install a plugin archive after validating its manifest and binary.

A URL or local path is a manual install and may include --sha256. A plugin ID
uses a configured signed marketplace; signed index and archive metadata are
mandatory for that path. Installing a plugin executes it with this user's
permissions; plugins are not a sandbox.`,
		Args:              cobra.ExactArgs(1),
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			if isURL(args[0]) || strings.ContainsAny(args[0], `/\\.`) {
				return a.doPluginInstall(cmd.Context(), args[0], sha256Flag, pluginInstallMeta{}, false)
			}
			return a.installFromMarket(cmd.Context(), args[0], versionFlag, marketURL)
		},
	}
	cmd.Flags().StringVar(&sha256Flag, "sha256", "", "Expected SHA-256 hex digest for a manual archive install")
	cmd.Flags().StringVar(&marketURL, "market", "", "Signed marketplace index URL")
	cmd.Flags().StringVar(&versionFlag, "version", "", "Exact marketplace plugin version")
	return cmd
}

func (a *app) doPluginInstall(ctx context.Context, source, expectedSHA256 string, meta pluginInstallMeta, isUpdate bool) error {
	archivePath, cleanUp, err := fetchPluginSource(ctx, source)
	if err != nil {
		return err
	}
	if cleanUp {
		defer os.Remove(archivePath)
	}
	if expectedSHA256 != "" {
		if err := verifySHA256(archivePath, expectedSHA256); err != nil {
			return err
		}
		fmt.Fprintln(a.output, "SHA-256 verified")
	} else {
		fmt.Fprintf(a.output, "Warning: no --sha256 provided for manual archive %q.\n", source)
	}
	staging, err := os.MkdirTemp("", "shiki-plugin-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	manifest, binaryName, err := extractPluginArchive(archivePath, staging)
	if err != nil {
		return fmt.Errorf("extracting plugin archive: %w", err)
	}
	if err := sdk.ValidateManifest(manifest); err != nil {
		return fmt.Errorf("invalid plugin manifest: %w", err)
	}
	if manifest.ID == "sync" || manifest.ID == "port" {
		return fmt.Errorf("cannot overwrite built-in plugin %q", manifest.ID)
	}
	meta.InstalledAt = time.Now().UTC().Format(time.RFC3339)
	if expectedSHA256 != "" {
		meta.SHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	}
	return a.commitPluginInstall(manifest, binaryName, staging, meta, isUpdate)
}

func (a *app) commitPluginInstall(manifest *sdk.PluginManifest, binaryName, staging string, meta pluginInstallMeta, isUpdate bool) error {
	destDir, err := installedPluginDir(manifest.ID)
	if err != nil {
		return err
	}
	base := filepath.Dir(destDir)
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(destDir); err == nil && !isUpdate {
		return fmt.Errorf("plugin %q is already installed; use `shiki plugins update %s` to upgrade", manifest.ID, manifest.ID)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(destDir); err == nil {
		fmt.Fprintf(a.output, "Upgrading plugin %q in %s\n", manifest.ID, destDir)
	}
	newDir, err := os.MkdirTemp(base, ".plugin-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(newDir)
	if err := pluginCopyFile(filepath.Join(staging, binaryName), filepath.Join(newDir, manifest.Binary), 0700, pluginMaxBinary); err != nil {
		return fmt.Errorf("installing binary: %w", err)
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(newDir, "manifest.json"), append(manifestData, '\n'), 0600); err != nil {
		return err
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(newDir, ".install-meta.json"), append(metaData, '\n'), 0600); err != nil {
		return err
	}
	oldDir := destDir + ".old-" + fmt.Sprint(time.Now().UnixNano())
	if _, err := os.Lstat(destDir); err == nil {
		if err := os.Rename(destDir, oldDir); err != nil {
			return fmt.Errorf("prepare plugin replacement: %w", err)
		}
	}
	if err := os.Rename(newDir, destDir); err != nil {
		if _, oldErr := os.Lstat(oldDir); oldErr == nil {
			_ = os.Rename(oldDir, destDir)
		}
		return fmt.Errorf("publish plugin: %w", err)
	}
	if oldDir != destDir {
		_ = os.RemoveAll(oldDir)
	}
	fmt.Fprintf(a.output, "Plugin %q (%s) installed to %s\n", manifest.ID, manifest.Version, destDir)
	fmt.Fprintf(a.output, "Run `shiki plugins enable %s --side local` to activate it.\n", manifest.ID)
	return nil
}

func (a *app) installFromMarket(ctx context.Context, id, version, marketURL string) error {
	if err := client.ValidatePluginMarketID(id); err != nil {
		return fmt.Errorf("invalid plugin id %q; use a URL/path for manual installation", id)
	}
	indexURL, err := a.selectMarketURL(marketURL)
	if err != nil {
		return err
	}
	store, err := a.loadPluginTrustStore()
	if err != nil {
		return err
	}
	index, err := fetchPluginIndex(ctx, indexURL, store)
	if err != nil {
		return err
	}
	entry, err := choosePluginEntry(index, id, version)
	if err != nil {
		return err
	}
	return a.installMarketEntry(ctx, indexURL, entry, store, false)
}

func (a *app) pluginUpdateCmd() *cobra.Command {
	var marketURL, version string
	cmd := &cobra.Command{
		Use: "update [plugin-id]", Short: "Update third-party plugins from signed marketplaces",
		Args: cobra.MaximumNArgs(1), PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := pluginDir()
			if err != nil {
				return err
			}
			ids := args
			if len(ids) == 0 {
				ids, err = listInstalledThirdPartyPlugins(base)
				if err != nil {
					return err
				}
			}
			if len(ids) == 0 {
				fmt.Fprintln(a.output, "No third-party plugins found.")
				return nil
			}
			var errs []string
			explicit := len(args) > 0
			for _, id := range ids {
				if err := a.updateOnePlugin(cmd.Context(), base, id, version, marketURL); err != nil {
					if errors.Is(err, errPluginNotMarket) && !explicit {
						fmt.Fprintf(a.output, "Plugin %q was installed manually; skipping automatic update.\n", id)
						continue
					}
					errs = append(errs, fmt.Sprintf("%s: %v", id, err))
				}
			}
			if len(errs) > 0 {
				return errors.New(strings.Join(errs, "\n"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&marketURL, "market", "", "Signed marketplace index URL override")
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
			if id == "sync" || id == "port" {
				return fmt.Errorf("cannot uninstall built-in plugin %q", id)
			}
			dir, err := installedPluginDir(id)
			if err != nil {
				return err
			}
			if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("plugin %q is not installed", id)
			} else if err != nil {
				return err
			}
			if err := a.confirm(cmd.Context(), "Remove plugin "+id+" from "+dir, yes); err != nil {
				return err
			}
			_ = a.disableLocalPlugin(cmd.Context(), id)
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("removing plugin directory: %w", err)
			}
			fmt.Fprintf(a.output, "Plugin %q removed. Configuration and state data are not affected.\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func (a *app) disableLocalPlugin(ctx context.Context, id string) error {
	return a.withClient(ctx, false, func(c *client.Client) error {
		target := &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}
		st, err := pluginStatus(ctx, c, target, id)
		if err != nil {
			return nil
		}
		_, err = c.Agent().ManagePlugin(ctx, &apb.ManagePluginRequest{Target: target, Request: &pb.ManagePluginRequest{PluginId: id, Action: pb.ManagePluginRequest_DISABLE, ExpectedRevision: st.Revision, RequestId: a.id()}})
		return err
	})
}

type pluginArchiveEntry struct{ name, stagingPath string }

func extractPluginArchive(archivePath, stagingDir string) (*sdk.PluginManifest, string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, "", fmt.Errorf("not a valid gzip archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var manifestData []byte
	var binaries []pluginArchiveEntry
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("reading archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		clean := path.Clean(strings.ReplaceAll(hdr.Name, "\\", "/"))
		parts := strings.Split(clean, "/")
		if strings.HasPrefix(clean, "/") || clean == "." || clean == ".." {
			return nil, "", fmt.Errorf("unsafe path in archive: %q", hdr.Name)
		}
		for _, part := range parts {
			if part == ".." || part == "" {
				return nil, "", fmt.Errorf("unsafe path in archive: %q", hdr.Name)
			}
		}
		name := filepath.Base(clean)
		if name == "manifest.json" {
			if manifestData != nil {
				return nil, "", errors.New("archive contains duplicate manifest.json")
			}
			manifestData, err = io.ReadAll(io.LimitReader(tr, pluginMaxManifest+1))
			if err != nil {
				return nil, "", err
			}
			if int64(len(manifestData)) > pluginMaxManifest {
				return nil, "", errors.New("manifest.json exceeds size limit")
			}
			continue
		}
		dest := filepath.Join(stagingDir, name)
		if err := writeExtractedFile(tr, dest, pluginMaxBinary); err != nil {
			return nil, "", fmt.Errorf("extracting %q: %w", name, err)
		}
		binaries = append(binaries, pluginArchiveEntry{name: name, stagingPath: dest})
	}
	if len(manifestData) == 0 {
		return nil, "", errors.New("archive does not contain manifest.json")
	}
	var manifest sdk.PluginManifest
	if err := client.DecodeStrictJSON(manifestData, &manifest); err != nil {
		return nil, "", fmt.Errorf("parsing manifest.json: %w", err)
	}
	for _, b := range binaries {
		if b.name == manifest.Binary {
			return &manifest, b.name, nil
		}
	}
	if len(binaries) == 1 {
		return &manifest, binaries[0].name, nil
	}
	names := make([]string, len(binaries))
	for i, b := range binaries {
		names[i] = b.name
	}
	return nil, "", fmt.Errorf("binary %q declared in manifest not found in archive (found: %s)", manifest.Binary, strings.Join(names, ", "))
}

func writeExtractedFile(r io.Reader, dest string, maxBytes int64) error {
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(r, maxBytes+1))
	if err != nil {
		return err
	}
	if n > maxBytes {
		return errors.New("file exceeds size limit")
	}
	return nil
}

func fetchPluginSource(ctx context.Context, source string) (string, bool, error) {
	if !isURL(source) {
		if _, err := os.Stat(source); err != nil {
			return "", false, fmt.Errorf("plugin source not found: %w", err)
		}
		return source, false, nil
	}
	tmp, err := os.CreateTemp("", "shiki-plugin-dl-*.tar.gz")
	if err != nil {
		return "", false, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", false, err
	}
	resp, err := (&http.Client{Timeout: pluginHTTPTimeout}).Do(req)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", false, fmt.Errorf("downloading plugin: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = os.Remove(tmpPath)
		return "", false, fmt.Errorf("downloading plugin: HTTP %d", resp.StatusCode)
	}
	out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", false, err
	}
	n, copyErr := io.Copy(out, io.LimitReader(resp.Body, pluginMaxArchiveBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", false, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", false, closeErr
	}
	if n > pluginMaxArchiveBytes {
		_ = os.Remove(tmpPath)
		return "", false, errors.New("plugin archive exceeds size limit")
	}
	return tmpPath, true, nil
}

func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

func verifySHA256(path, expected string) error {
	clean := strings.ToLower(strings.TrimSpace(expected))
	if len(clean) != 64 {
		return errors.New("--sha256 must be a 64-character lowercase hexadecimal digest")
	}
	if _, err := hex.DecodeString(clean); err != nil {
		return errors.New("--sha256 must be hexadecimal")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != clean {
		return fmt.Errorf("SHA-256 mismatch: expected %s, got %s", clean, actual)
	}
	return nil
}

func pluginCopyFile(src, dst string, mode os.FileMode, maxBytes int64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(in, maxBytes+1))
	if err != nil {
		return err
	}
	if n > maxBytes {
		return errors.New("file exceeds size limit")
	}
	return nil
}
