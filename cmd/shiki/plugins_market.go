package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/spf13/cobra"
)

const (
	pluginMarketConfigFormat   = 1
	pluginMarketMaxURLs        = 64
	pluginMarketConfigMaxBytes = 64 << 10
)

var errPluginNotMarket = errors.New("plugin was not installed from a signed marketplace")

type pluginMarketConfig struct {
	Format int      `json:"format"`
	URLs   []string `json:"urls"`
}

func pluginStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".shiki", "plugin-market"), nil
}

func pluginTrustStorePath() (string, error) {
	dir, err := pluginStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "trust.json"), nil
}

func pluginMarketConfigPath() (string, error) {
	dir, err := pluginStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "markets.json"), nil
}

func (a *app) loadPluginTrustStore() (client.PluginMarketTrustStore, error) {
	path, err := pluginTrustStorePath()
	if err != nil {
		return client.PluginMarketTrustStore{}, err
	}
	store, err := client.LoadPluginMarketTrustStore(path)
	if err != nil {
		return client.PluginMarketTrustStore{}, fmt.Errorf("cannot load plugin trust store %q; add a publisher key with `shiki plugins trust add`: %w", path, err)
	}
	return store, nil
}

func loadPluginMarkets() (pluginMarketConfig, error) {
	path, err := pluginMarketConfigPath()
	if err != nil {
		return pluginMarketConfig{}, err
	}
	data, err := os.ReadFile(path)
	if len(data) > pluginMarketConfigMaxBytes {
		return pluginMarketConfig{}, errors.New("plugin marketplace configuration is too large")
	}
	if errors.Is(err, os.ErrNotExist) {
		return pluginMarketConfig{Format: pluginMarketConfigFormat, URLs: []string{}}, nil
	}
	if err != nil {
		return pluginMarketConfig{}, err
	}
	var cfg pluginMarketConfig
	if err := client.DecodeStrictJSON(data, &cfg); err != nil || cfg.Format != pluginMarketConfigFormat {
		return pluginMarketConfig{}, errors.New("invalid plugin marketplace configuration")
	}
	if len(cfg.URLs) > pluginMarketMaxURLs {
		return pluginMarketConfig{}, errors.New("too many plugin marketplaces")
	}
	seen := make(map[string]struct{}, len(cfg.URLs))
	for _, raw := range cfg.URLs {
		if len(raw) == 0 || len(raw) > client.PluginMarketMaxURLLength {
			return pluginMarketConfig{}, errors.New("invalid plugin marketplace URL")
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return pluginMarketConfig{}, errors.New("invalid plugin marketplace URL")
		}
		if _, ok := seen[raw]; ok {
			return pluginMarketConfig{}, errors.New("duplicate plugin marketplace URL")
		}
		seen[raw] = struct{}{}
	}
	return cfg, nil
}

func savePluginMarkets(cfg pluginMarketConfig) error {
	if cfg.Format == 0 {
		cfg.Format = pluginMarketConfigFormat
	}
	if cfg.Format != pluginMarketConfigFormat {
		return errors.New("invalid plugin marketplace configuration")
	}
	if len(cfg.URLs) > pluginMarketMaxURLs {
		return errors.New("too many plugin marketplaces")
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > pluginMarketConfigMaxBytes {
		return errors.New("plugin marketplace configuration is too large")
	}
	dir, err := pluginStateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".markets-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, "markets.json"))
}

func (a *app) selectMarketURL(explicit string) (string, error) {
	if explicit != "" {
		if !isHTTPSPluginURL(explicit) {
			return "", errors.New("marketplace URL must use HTTPS")
		}
		return explicit, nil
	}
	cfg, err := loadPluginMarkets()
	if err != nil {
		return "", err
	}
	if len(cfg.URLs) == 0 {
		return "", errors.New("no plugin marketplace configured; add one with `shiki plugins market add <https-url>`")
	}
	return cfg.URLs[0], nil
}

func isHTTPSPluginURL(raw string) bool {
	if len(raw) == 0 || len(raw) > client.PluginMarketMaxURLLength {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}

func fetchPluginBytes(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if !isHTTPSPluginURL(raw) {
		return nil, errors.New("marketplace resource URL must use HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: pluginHTTPTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", raw, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %s", raw, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s exceeds size limit", raw)
	}
	return data, nil
}

func fetchPluginIndex(ctx context.Context, raw string, store client.PluginMarketTrustStore) (client.PluginMarketIndex, error) {
	data, err := fetchPluginBytes(ctx, raw, client.PluginMarketMaxEnvelopeBytes)
	if err != nil {
		return client.PluginMarketIndex{}, err
	}
	index, err := client.VerifyPluginMarketIndex(data, store)
	if err != nil {
		return client.PluginMarketIndex{}, fmt.Errorf("verify marketplace index: %w", err)
	}
	return index, nil
}

func choosePluginEntry(index client.PluginMarketIndex, id, version string) (client.PluginMarketEntry, error) {
	entries := make([]client.PluginMarketEntry, 0)
	for _, entry := range index.Entries {
		if entry.ID == id && entry.OS == runtime.GOOS && entry.Arch == runtime.GOARCH && (version == "" || entry.Version == version) {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		if version == "" {
			return client.PluginMarketEntry{}, fmt.Errorf("no signed %s plugin for %s/%s", id, runtime.GOOS, runtime.GOARCH)
		}
		return client.PluginMarketEntry{}, fmt.Errorf("no signed %s plugin version %q for %s/%s", id, version, runtime.GOOS, runtime.GOARCH)
	}
	sort.Slice(entries, func(i, j int) bool { return comparePluginVersions(entries[i].Version, entries[j].Version) > 0 })
	return entries[0], nil
}

func comparePluginVersions(a, b string) int {
	if a == b {
		return 0
	}
	ac, ap := splitPluginVersion(a)
	bc, bp := splitPluginVersion(b)
	for i := 0; i < len(ac) || i < len(bc); i++ {
		av, bv := "0", "0"
		if i < len(ac) {
			av = ac[i]
		}
		if i < len(bc) {
			bv = bc[i]
		}
		if av != bv {
			if isDigits(av) && isDigits(bv) {
				av = strings.TrimLeft(av, "0")
				bv = strings.TrimLeft(bv, "0")
				if av == "" {
					av = "0"
				}
				if bv == "" {
					bv = "0"
				}
				if len(av) != len(bv) {
					if len(av) > len(bv) {
						return 1
					}
					return -1
				}
			}
			if av > bv {
				return 1
			}
			return -1
		}
	}
	if ap == bp {
		return 0
	}
	if ap == "" {
		return 1
	}
	if bp == "" {
		return -1
	}
	if ap > bp {
		return 1
	}
	return -1
}

func splitPluginVersion(value string) ([]string, string) {
	value = strings.SplitN(value, "+", 2)[0]
	parts := strings.SplitN(value, "-", 2)
	core := strings.Split(parts[0], ".")
	pre := ""
	if len(parts) == 2 {
		pre = parts[1]
	}
	return core, pre
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (a *app) installMarketEntry(ctx context.Context, marketURL string, entry client.PluginMarketEntry, store client.PluginMarketTrustStore, update bool) error {
	archivePath, cleanUp, err := fetchPluginSource(ctx, entry.URL)
	if err != nil {
		return err
	}
	if cleanUp {
		defer os.Remove(archivePath)
	}
	canonical := filepath.Join(filepath.Dir(archivePath), fmt.Sprintf("%s-%s-%s-%s.tar.gz", entry.ID, entry.Version, entry.OS, entry.Arch))
	if err := os.Rename(archivePath, canonical); err != nil {
		return fmt.Errorf("prepare downloaded archive: %w", err)
	}
	defer os.Remove(canonical)
	signature, err := fetchPluginBytes(ctx, entry.SignatureURL, client.PluginMarketMaxArchiveSignatureBytes)
	if err != nil {
		return err
	}
	if err := client.VerifyPluginMarketArchive(canonical, signature, entry, store); err != nil {
		return fmt.Errorf("verify plugin archive: %w", err)
	}
	meta := pluginInstallMeta{SourceURL: entry.URL, MarketURL: marketURL, SignatureURL: entry.SignatureURL, KeyID: entry.KeyID}
	return a.doPluginInstall(ctx, canonical, entry.SHA256, meta, update)
}

func listInstalledThirdPartyPlugins(base string) ([]string, error) {
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "sync" || entry.Name() == "port" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if validPluginID(entry.Name()) {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (a *app) updateOnePlugin(ctx context.Context, base, id, requestedVersion, marketURL string) error {
	if !validPluginID(id) {
		return errors.New("invalid plugin id")
	}
	metaPath := filepath.Join(base, id, ".install-meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("cannot read install metadata: %w", err)
	}
	var meta pluginInstallMeta
	if err := client.DecodeStrictJSON(data, &meta); err != nil || meta.MarketURL == "" {
		return errPluginNotMarket
	}
	if marketURL == "" {
		marketURL = meta.MarketURL
	}
	store, err := a.loadPluginTrustStore()
	if err != nil {
		return err
	}
	index, err := fetchPluginIndex(ctx, marketURL, store)
	if err != nil {
		return err
	}
	entry, err := choosePluginEntry(index, id, requestedVersion)
	if err != nil {
		return err
	}
	currentVersion := metaVersion(id, base)
	comparison := comparePluginVersions(entry.Version, currentVersion)
	if comparison < 0 {
		return fmt.Errorf("refusing marketplace downgrade from %s to %s", currentVersion, entry.Version)
	}
	if comparison == 0 {
		fmt.Fprintf(a.output, "Plugin %q is already up to date (%s).\n", id, currentVersion)
		return nil
	}
	return a.installMarketEntry(ctx, marketURL, entry, store, true)
}

func metaVersion(id, base string) string {
	data, err := os.ReadFile(filepath.Join(base, id, "manifest.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if client.DecodeStrictJSON(data, &manifest) != nil {
		return ""
	}
	return manifest.Version
}

func (a *app) pluginTrustCmd() *cobra.Command {
	root := &cobra.Command{Use: "trust", Short: "Manage trusted plugin publisher keys", PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	var add = &cobra.Command{Use: "add <key-id> <public-key.pem>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		path, err := pluginTrustStorePath()
		if err != nil {
			return err
		}
		store := client.NewPluginMarketTrustStore()
		if _, statErr := os.Stat(path); statErr == nil {
			existing, loadErr := client.LoadPluginMarketTrustStore(path)
			if loadErr != nil {
				return fmt.Errorf("cannot load existing plugin trust store: %w", loadErr)
			}
			store = existing
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if err := client.AddPluginMarketTrustRoot(&store, client.PluginMarketTrustRoot{KeyID: args[0], PublicKey: string(data)}); err != nil {
			return err
		}
		if err := client.SavePluginMarketTrustStore(path, store); err != nil {
			return err
		}
		fmt.Fprintf(a.output, "Trusted plugin publisher key %q.\n", args[0])
		return nil
	}}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		path, err := pluginTrustStorePath()
		if err != nil {
			return err
		}
		store, err := client.LoadPluginMarketTrustStore(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintln(a.output, "No trusted plugin publisher keys.")
				return nil
			}
			return err
		}
		ids := make([]string, 0, len(store.Roots))
		for _, root := range store.Roots {
			ids = append(ids, root.KeyID)
		}
		if a.json {
			return a.printJSON(ids)
		}
		for _, id := range ids {
			fmt.Fprintln(a.output, id)
		}
		return nil
	}}
	root.AddCommand(add, list)
	return root
}

func (a *app) pluginMarketCmd() *cobra.Command {
	root := &cobra.Command{Use: "market", Short: "Manage signed plugin marketplaces", PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	add := &cobra.Command{Use: "add <https-index-url>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := a.loadPluginTrustStore()
		if err != nil {
			return err
		}
		if _, err := fetchPluginIndex(cmd.Context(), args[0], store); err != nil {
			return err
		}
		cfg, err := loadPluginMarkets()
		if err != nil {
			return err
		}
		for _, existing := range cfg.URLs {
			if existing == args[0] {
				return nil
			}
		}
		cfg.URLs = append(cfg.URLs, args[0])
		sort.Strings(cfg.URLs)
		if err := savePluginMarkets(cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.output, "Added signed plugin marketplace %s.\n", args[0])
		return nil
	}}
	remove := &cobra.Command{Use: "remove <https-index-url>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := loadPluginMarkets()
		if err != nil {
			return err
		}
		out := cfg.URLs[:0]
		found := false
		for _, existing := range cfg.URLs {
			if existing == args[0] {
				found = true
				continue
			}
			out = append(out, existing)
		}
		if !found {
			return fmt.Errorf("marketplace is not configured: %s", args[0])
		}
		cfg.URLs = out
		if err := savePluginMarkets(cfg); err != nil {
			return err
		}
		return nil
	}}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		cfg, err := loadPluginMarkets()
		if err != nil {
			return err
		}
		if a.json {
			return a.printJSON(cfg.URLs)
		}
		for _, value := range cfg.URLs {
			fmt.Fprintln(a.output, value)
		}
		return nil
	}}
	search := &cobra.Command{Use: "search [query]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) == 1 {
			query = strings.ToLower(args[0])
		}
		cfg, err := loadPluginMarkets()
		if err != nil {
			return err
		}
		store, err := a.loadPluginTrustStore()
		if err != nil {
			return err
		}
		type result struct {
			Market string                   `json:"market"`
			Entry  client.PluginMarketEntry `json:"entry"`
		}
		results := make([]result, 0)
		for _, market := range cfg.URLs {
			index, fetchErr := fetchPluginIndex(cmd.Context(), market, store)
			if fetchErr != nil {
				return fetchErr
			}
			for _, entry := range index.Entries {
				if query == "" || strings.Contains(strings.ToLower(entry.ID), query) || strings.Contains(strings.ToLower(index.Publisher), query) {
					results = append(results, result{market, entry})
				}
			}
		}
		if a.json {
			return a.printJSON(results)
		}
		for _, item := range results {
			fmt.Fprintf(a.output, "%s %s %s/%s %s\n", item.Entry.ID, item.Entry.Version, item.Entry.OS, item.Entry.Arch, item.Market)
		}
		return nil
	}}
	root.AddCommand(add, remove, list, search)
	return root
}
