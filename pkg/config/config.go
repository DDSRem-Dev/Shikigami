// Package config owns the versioned, strict configuration shared by commands
// and services. Credentials are stored separately and never appear in this schema.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = 1

type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return errors.New("duration must be a Go duration string")
	}
	parsed, err := time.ParseDuration(node.Value)
	if err != nil || parsed <= 0 {
		return errors.New("duration must be positive and use h/m/s units")
	}
	*d = Duration(parsed)
	return nil
}
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }
func (d Duration) Value() time.Duration      { return time.Duration(d) }

type Daemon struct {
	SchemaVersion  int            `yaml:"schema_version"`
	RunUser        string         `yaml:"run_user"`
	StateDir       string         `yaml:"state_dir"`
	ProjectsDir    string         `yaml:"projects_dir"`
	PluginsDir     string         `yaml:"plugins_dir"`
	Server         DaemonServer   `yaml:"server"`
	Tailscale      Tailscale      `yaml:"tailscale"`
	ResourceLimits ResourceLimits `yaml:"resource_limits"`
	AutoCleanup    AutoCleanup    `yaml:"auto_cleanup"`
	Claude         Claude         `yaml:"claude"`
	Revision       string         `yaml:"-"`
	MigratedFrom   *int           `yaml:"-"`
}
type DaemonServer struct {
	Port      int       `yaml:"port"`
	DirectTLS DirectTLS `yaml:"direct_tls"`
}
type DirectTLS struct {
	Enabled  bool   `yaml:"enabled"`
	Address  string `yaml:"address"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}
type Tailscale struct {
	Enabled    bool   `yaml:"enabled"`
	Hostname   string `yaml:"hostname"`
	StateDir   string `yaml:"state_dir"`
	ControlURL string `yaml:"control_url"`
}
type ResourceLimits struct {
	MaxSessions         int `yaml:"max_sessions"`
	MaxConcurrentClaude int `yaml:"max_concurrent_claude"`
}
type Claude struct {
	Executable  string      `yaml:"executable"`
	Args        []string    `yaml:"args"`
	AutoRestart AutoRestart `yaml:"auto_restart"`
}
type AutoCleanup struct {
	Enabled           bool     `yaml:"enabled"`
	IdleThreshold     Duration `yaml:"idle_threshold"`
	WarnBeforeCleanup Duration `yaml:"warn_before_cleanup"`
}

type AutoRestart struct {
	Enabled      bool     `yaml:"enabled"`
	MaxAttempts  int      `yaml:"max_attempts"`
	Backoff      Duration `yaml:"backoff"`
	StableWindow Duration `yaml:"stable_window"`
}

type Client struct {
	Templates     map[string]SessionTemplate `yaml:"templates,omitempty"`
	PluginsDir    string                     `yaml:"plugins_dir"`
	SchemaVersion int                        `yaml:"schema_version"`
	Agent         Agent                      `yaml:"agent"`
	Tailscale     Tailscale                  `yaml:"tailscale"`
	Servers       map[string]Server          `yaml:"servers"`
	Projects      map[string]ProjectMapping  `yaml:"projects"`
	Revision      string                     `yaml:"-"`
	MigratedFrom  *int                       `yaml:"-"`
}
type Agent struct {
	Socket    string `yaml:"socket"`
	StateDir  string `yaml:"state_dir"`
	AutoStart bool   `yaml:"auto_start"`
}
type Server struct {
	TailscaleAddress string `yaml:"tailscale_address"`
	TokenRef         string `yaml:"token_ref"`
	DirectTLSAddress string `yaml:"direct_tls_address"`
	TLSServerName    string `yaml:"tls_server_name"`
	TLSCAFile        string `yaml:"tls_ca_file"`
	DaemonID         string `yaml:"daemon_id"`
}
type ProjectMapping struct {
	Server         string            `yaml:"server"`
	ProjectID      string            `yaml:"project_id"`
	DefaultSession string            `yaml:"default_session"`
	SessionHints   map[string]string `yaml:"session_hints"`
	BackgroundSync bool              `yaml:"background_sync"`
}

func DefaultDaemon(runUser, stateDir string) *Daemon {
	return &Daemon{AutoCleanup: AutoCleanup{IdleThreshold: Duration(168 * time.Hour), WarnBeforeCleanup: Duration(24 * time.Hour)}, SchemaVersion: SchemaVersion, RunUser: runUser, StateDir: stateDir, Server: DaemonServer{Port: 9527}, Tailscale: Tailscale{Enabled: true, Hostname: "shiki-vps", ControlURL: "https://controlplane.tailscale.com"}, ResourceLimits: ResourceLimits{MaxSessions: 10, MaxConcurrentClaude: 5}, Claude: Claude{Executable: "claude", Args: []string{}, AutoRestart: AutoRestart{MaxAttempts: 3, Backoff: Duration(time.Second), StableWindow: Duration(time.Minute)}}}
}
func DefaultClient() *Client {
	return &Client{SchemaVersion: SchemaVersion, PluginsDir: "~/.shiki/plugins", Agent: Agent{Socket: "~/.shiki/run/agent.sock", StateDir: "~/.shiki/agent", AutoStart: true}, Tailscale: Tailscale{Enabled: true, Hostname: "shiki-client", StateDir: "~/.shiki/tailscale", ControlURL: "https://controlplane.tailscale.com"}, Servers: map[string]Server{}, Projects: map[string]ProjectMapping{}}
}

// NormalizeAndValidate resolves ~ against the daemon user's home, never the
// installer's HOME, and validates the complete candidate before it is used.
func (c *Daemon) NormalizeAndValidate() error {
	if c.SchemaVersion != SchemaVersion {
		return errors.New("unsupported daemon config schema")
	}
	account, err := user.Lookup(c.RunUser)
	if err != nil {
		return errors.New("run_user must identify an existing OS user")
	}
	if account.Uid == "0" {
		return errors.New("daemon run_user must not be root")
	}
	if c.StateDir, err = absolutePath(c.StateDir, account.HomeDir); err != nil {
		return fmt.Errorf("state_dir: %w", err)
	}
	if c.ProjectsDir == "" {
		c.ProjectsDir = filepath.Join(account.HomeDir, "shiki-projects")
	}
	if c.ProjectsDir, err = absolutePath(c.ProjectsDir, account.HomeDir); err != nil {
		return fmt.Errorf("projects_dir: %w", err)
	}
	if c.PluginsDir == "" {
		c.PluginsDir = filepath.Join(c.StateDir, "plugins")
	}
	if c.PluginsDir, err = absolutePath(c.PluginsDir, account.HomeDir); err != nil {
		return fmt.Errorf("plugins_dir: %w", err)
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return errors.New("server.port must be between 1 and 65535")
	}
	tls := &c.Server.DirectTLS
	if tls.Enabled {
		if err = address(tls.Address, false); err != nil {
			return fmt.Errorf("server.direct_tls.address: %w", err)
		}
		if tls.CertFile, err = absolutePath(tls.CertFile, account.HomeDir); err != nil {
			return fmt.Errorf("server.direct_tls.cert_file: %w", err)
		}
		if tls.KeyFile, err = absolutePath(tls.KeyFile, account.HomeDir); err != nil {
			return fmt.Errorf("server.direct_tls.key_file: %w", err)
		}
	} else if tls.Address != "" || tls.CertFile != "" || tls.KeyFile != "" {
		return errors.New("direct TLS settings require enabled: true")
	}
	if !tls.Enabled && !c.Tailscale.Enabled {
		return errors.New("enable tailscale or direct TLS transport")
	}
	if c.Tailscale.StateDir == "" {
		c.Tailscale.StateDir = filepath.Join(c.StateDir, "tailscale")
	}
	if err = c.Tailscale.normalize(account.HomeDir); err != nil {
		return err
	}
	if c.ResourceLimits.MaxSessions < 1 || c.ResourceLimits.MaxConcurrentClaude < 1 || c.ResourceLimits.MaxConcurrentClaude > c.ResourceLimits.MaxSessions {
		return errors.New("resource limits require 1 <= max_concurrent_claude <= max_sessions")
	}
	if c.Claude.Executable == "" || strings.IndexByte(c.Claude.Executable, 0) >= 0 {
		return errors.New("claude.executable is required")
	}
	if strings.ContainsAny(c.Claude.Executable, "/\\") || strings.HasPrefix(c.Claude.Executable, "~") {
		if c.Claude.Executable, err = absolutePath(c.Claude.Executable, account.HomeDir); err != nil {
			return fmt.Errorf("claude.executable: %w", err)
		}
	}
	for _, arg := range c.Claude.Args {
		if strings.IndexByte(arg, 0) >= 0 {
			return errors.New("claude.args cannot contain NUL")
		}
	}
	if c.AutoCleanup.IdleThreshold <= 0 || c.AutoCleanup.WarnBeforeCleanup < Duration(time.Second) || c.AutoCleanup.WarnBeforeCleanup >= c.AutoCleanup.IdleThreshold {
		return errors.New("auto_cleanup requires 1s <= warn_before_cleanup < idle_threshold")
	}
	r := c.Claude.AutoRestart
	if r.MaxAttempts < 1 || r.MaxAttempts > 100 || r.Backoff <= 0 || r.StableWindow <= 0 {
		return errors.New("claude.auto_restart requires max_attempts 1–100 and positive durations")
	}
	return nil
}

// CheckRunUser belongs at daemon startup, not configuration inspection during
// installation. This package never changes process credentials.
func (c *Daemon) CheckRunUser() error {
	account, err := user.Lookup(c.RunUser)
	if err != nil {
		return err
	}
	if account.Uid == "0" || account.Uid != strconv.Itoa(os.Geteuid()) {
		return errors.New("daemon must run as configured ordinary run_user")
	}
	return nil
}

func (c *Client) NormalizeAndValidate() error {
	if c.SchemaVersion != SchemaVersion {
		return errors.New("unsupported client config schema")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if c.Agent.StateDir, err = absolutePath(c.Agent.StateDir, home); err != nil {
		return fmt.Errorf("agent.state_dir: %w", err)
	}
	if c.PluginsDir == "" {
		c.PluginsDir = filepath.Join(home, ".shiki", "plugins")
	}
	if c.PluginsDir, err = absolutePath(c.PluginsDir, home); err != nil {
		return fmt.Errorf("plugins_dir: %w", err)
	}
	if c.Agent.Socket, err = absolutePath(c.Agent.Socket, home); err != nil {
		return fmt.Errorf("agent.socket: %w", err)
	}
	if len(c.Agent.Socket) > 100 {
		return errors.New("agent.socket path exceeds portable Unix socket limit (100 bytes)")
	}
	if err = c.Tailscale.normalize(home); err != nil {
		return err
	}
	for name, server := range c.Servers {
		if !identifier.MatchString(name) {
			return errors.New("server names must use letters, digits, dash, underscore or dot")
		}
		if server.TailscaleAddress != "" {
			if !c.Tailscale.Enabled {
				return errors.New("tailscale_address requires tailscale.enabled")
			}
			if err = address(server.TailscaleAddress, true); err != nil {
				return fmt.Errorf("server tailscale_address: %w", err)
			}
		}
		if server.DirectTLSAddress != "" {
			if err = address(server.DirectTLSAddress, true); err != nil {
				return fmt.Errorf("server direct_tls_address: %w", err)
			}
			if server.TLSServerName == "" || strings.ContainsAny(server.TLSServerName, "/\\ :\t\r\n") {
				return errors.New("direct TLS requires a valid tls_server_name")
			}
		} else if server.TLSServerName != "" || server.TLSCAFile != "" {
			return errors.New("TLS trust settings require direct_tls_address")
		}
		if server.TailscaleAddress == "" && server.DirectTLSAddress == "" {
			return errors.New("server requires a tailscale or direct TLS address")
		}
		if server.TLSCAFile != "" {
			if server.TLSCAFile, err = absolutePath(server.TLSCAFile, home); err != nil {
				return fmt.Errorf("tls_ca_file: %w", err)
			}
		}
		if server.TokenRef != "" && !identifier.MatchString(server.TokenRef) {
			return errors.New("token_ref must be a credential identifier, not a token or path")
		}
		if strings.HasPrefix(server.TokenRef, "shiki_") {
			return errors.New("raw tokens cannot be stored in configuration")
		}
		if server.DaemonID != "" && !strings.HasPrefix(server.DaemonID, "daemon_") {
			return errors.New("invalid daemon_id")
		}
		c.Servers[name] = server
	}
	projects := map[string]ProjectMapping{}
	for path, mapping := range c.Projects {
		root, err := absolutePath(path, home)
		if err != nil {
			return fmt.Errorf("project mapping path: %w", err)
		}
		if _, exists := projects[root]; exists {
			return errors.New("duplicate normalized project root")
		}
		if _, ok := c.Servers[mapping.Server]; !ok {
			return errors.New("project mapping references an unknown server")
		}
		if !strings.HasPrefix(mapping.ProjectID, "proj_") {
			return errors.New("project mapping requires a proj_ project ID")
		}
		for path, session := range mapping.SessionHints {
			if !validRelative(path) || session == "" {
				return errors.New("session_hints require clean relative directories and session names")
			}
		}
		projects[root] = mapping
	}
	c.Projects = projects
	return c.validateTemplates()
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var hostname = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func (t *Tailscale) normalize(home string) error {
	var err error
	if t.StateDir, err = absolutePath(t.StateDir, home); err != nil {
		return fmt.Errorf("tailscale.state_dir: %w", err)
	}
	if !t.Enabled {
		return nil
	}
	if !hostname.MatchString(t.Hostname) {
		return errors.New("tailscale.hostname must be a DNS label")
	}
	u, err := url.Parse(t.ControlURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("tailscale.control_url must be an HTTPS control-plane URL without credentials")
	}
	return nil
}

func absolutePath(path, home string) (string, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return "", errors.New("an absolute path is required")
	}
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	} else if strings.HasPrefix(path, "~") {
		return "", errors.New("only ~ and ~/ path expansion are supported")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("relative paths are not allowed")
	}
	return filepath.Clean(path), nil
}
func address(value string, requireHost bool) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil || strings.ContainsAny(host, "/\\@ \t\r\n") {
		return errors.New("address must be host:port without a URL scheme")
	}
	if requireHost && host == "" {
		return errors.New("address requires a host")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}
func validRelative(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\\x00") || filepath.Clean(path) != path {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." || part == "." || part == "" {
			return false
		}
	}
	return true
}
