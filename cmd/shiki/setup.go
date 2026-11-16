package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// setupBackend keeps onboarding on the same CLI/agent authorization and revision
// paths as individual commands. It never reads or edits host/plugin state files.
type setupBackend interface {
	run(context.Context, bool, ...string) error
	query(context.Context, proto.Message, ...string) error
	connect(context.Context, string, bool, string) error
	health(context.Context, string) error
}

type setupCLI struct{ app *app }

func (s setupCLI) execute(ctx context.Context, out io.Writer, args ...string) error {
	root := newRoot(s.app.input, out, s.app.errorOutput)
	root.SetArgs(append([]string{"--config", s.app.configPath, "--timeout", s.app.timeout.String()}, args...))
	return root.ExecuteContext(ctx)
}

func (s setupCLI) run(ctx context.Context, visible bool, args ...string) error {
	var out io.Writer = io.Discard
	if visible {
		out = s.app.output
	}
	return s.execute(ctx, out, args...)
}

func (s setupCLI) query(ctx context.Context, result proto.Message, args ...string) error {
	var out bytes.Buffer
	if err := s.execute(ctx, &out, append([]string{"--json"}, args...)...); err != nil {
		return err
	}
	return protojson.Unmarshal(out.Bytes(), result)
}

func (s setupCLI) connect(ctx context.Context, server string, renew bool, address string) error {
	if renew {
		token, err := s.app.credential(ctx, &serverFlags{}, true)
		if err != nil {
			return err
		}
		defer clear(token)
		if err := s.app.withClient(ctx, false, func(c *client.Client) error {
			record, err := c.ResolveServer(ctx, server)
			if err != nil {
				return err
			}
			_, err = c.Agent().SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: token, ExpectedCredentialRevision: record.CredentialRevision, RequestId: s.app.id()})
			return err
		}); err != nil {
			return err
		}
	}
	args := []string{"connect", server}
	if address != "" {
		args = append(args, "--tailscale-address", address)
	}
	return s.run(ctx, true, args...)
}

func (s setupCLI) health(ctx context.Context, server string) error {
	return s.app.withClient(ctx, false, func(c *client.Client) error {
		status, err := c.Agent().GetStatus(ctx, &pb.Empty{})
		if err != nil {
			return err
		}
		if status.State != "ready" {
			return fmt.Errorf("local agent is %s: %s", safeText(status.State), safeText(status.ReasonCode))
		}
		record, err := c.ResolveServer(ctx, server)
		if err != nil {
			return err
		}
		health, err := c.Remote(record.Config.Id).HealthCheck(ctx, &pb.Empty{})
		if err != nil {
			return err
		}
		if health.Status != pb.HealthStatus_HEALTHY {
			return errors.New("remote core is not healthy; inspect shiki doctor and rerun setup after resolving its reported prerequisites")
		}
		return nil
	})
}

type setupWizard struct {
	backend setupBackend
	prompt  func(context.Context, string, string) (string, error)
	out     io.Writer
	address string
}

func (a *app) setupCommand() *cobra.Command {
	var components, address string
	command := &cobra.Command{
		Use: "setup", Args: cobra.NoArgs,
		Short: "Connect a server and configure the selected plugins in one guided session",
		Long:  "Interactive setup after installing the local agent and VPS. Connects through the agent, enables selected plugins and can open a new remote browser. Existing plugin configuration, profiles, files and whole-host region settings are preserved unless explicitly changed. Account authorization remains interactive. Rerun the same command to continue after fixing a reported prerequisite.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if components != "" && components != "all" && components != "core" {
				return errors.New("--components must be all or core")
			}
			if address != "" {
				var err error
				address, err = normalizeTailnetAddress(address)
				if err != nil {
					return err
				}
			}
			if a.json || a.project != "" || a.requestID != "" {
				return errors.New("setup is an interactive sequence; --json, --project and --request-id do not apply")
			}
			if os.Geteuid() == 0 {
				return errors.New("run shiki setup as the ordinary client user, without sudo")
			}
			interactive := *a
			if !interactive.interactive() {
				tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
				if err != nil || !term.IsTerminal(int(tty.Fd())) {
					if tty != nil {
						tty.Close()
					}
					return errors.New("setup requires a controlling terminal; use connect and plugins commands with explicit arguments for automation")
				}
				defer tty.Close()
				interactive.input, interactive.output, interactive.errorOutput = tty, tty, tty
			}
			wizard := setupWizard{backend: setupCLI{app: &interactive}, prompt: interactive.prompt, out: interactive.output, address: address}
			return wizard.setup(cmd.Context(), components, a.server)
		},
	}
	command.Flags().StringVar(&components, "components", "", "all (sync, port, region, browser) or core (sync, port); omitted prompts")
	command.Flags().StringVar(&address, "tailscale-address", "", "Explicit server address correction; preserves saved credentials and pinned daemon identity")
	return command
}

func (w *setupWizard) setup(ctx context.Context, components, server string) error {
	fmt.Fprintln(w.out, "Shikigami setup — connect once, then configure your plugins.")
	if components == "" {
		var err error
		components, err = w.prompt(ctx, "Plugins: all (recommended; VPS browser/region support required) or core", "all")
		if err != nil {
			return err
		}
	}
	if components != "all" && components != "core" {
		return errors.New("select all or core; no configuration changed")
	}
	if err := w.backend.run(ctx, false, "agent", "start"); err != nil {
		return fmt.Errorf("start local agent: %w", err)
	}
	if err := w.node(ctx); err != nil {
		return err
	}
	list := new(apb.ServerList)
	if err := w.backend.query(ctx, list, "servers", "list"); err != nil {
		return err
	}
	if server == "" {
		defaultName := "my-vps"
		for _, record := range list.Servers {
			fmt.Fprintf(w.out, "Existing server: %s\n", safeText(record.GetConfig().GetName()))
		}
		if len(list.Servers) == 1 {
			defaultName = list.Servers[0].GetConfig().GetName()
		}
		var err error
		server, err = w.prompt(ctx, "Server label (e.g. my-vps; an existing label resumes its saved connection)", defaultName)
		if err != nil {
			return err
		}
	}
	if server == "" {
		return errors.New("server name is required")
	}
	existing, renew := false, false
	for _, record := range list.Servers {
		if record.GetConfig().GetName() == server || record.GetConfig().GetId() == server {
			existing = true
			if saved := record.GetConfig().GetTailscaleAddress(); saved != "" {
				fmt.Fprintf(w.out, "Saved VPS address: %s (change with setup --tailscale-address <IP-or-hostname>)\n", safeText(saved))
				if _, err := normalizeTailnetAddress(saved); err != nil && w.address == "" {
					fmt.Fprintln(w.out, "The saved address is invalid. Enter the VPS address to correct this record; its credential and pinned identity are retained.")
					for {
						answer, err := w.prompt(ctx, "VPS Tailscale IP or hostname (not the port number)", "")
						if err != nil {
							return err
						}
						w.address, err = normalizeTailnetAddress(answer)
						if err == nil {
							break
						}
						fmt.Fprintln(w.out, err)
					}
				}
			}
			// A saved status can still reflect the node login that just finished.
			// Device authorization never means its application Token was rejected.
			renew = record.CredentialPresent && record.ConnectionState == "auth_required" && record.ReasonCode != "NODE_AUTH_REQUIRED"
		}
	}
	if renew {
		answer, err := w.confirm(ctx, "Saved server credential needs authorization. Replace it with a fresh token?", true)
		if err != nil {
			return err
		}
		if !answer {
			return errors.New("server authorization remains pending; saved credential preserved")
		}
	}
	if err := w.backend.connect(ctx, server, renew, w.address); err != nil {
		return fmt.Errorf("server connection remains pending; check the displayed address and shiki doctor, then rerun setup: %w", err)
	}
	if err := w.backend.run(ctx, false, "servers", "switch", server); err != nil {
		return err
	}
	if err := w.plugins(ctx, server, components); err != nil {
		return err
	}
	if err := w.ports(ctx, server, existing); err != nil {
		return err
	}
	if components == "all" {
		if err := w.browser(ctx, server); err != nil {
			return err
		}
	}
	if err := w.backend.health(ctx, server); err != nil {
		return fmt.Errorf("configuration saved, but application readiness remains pending: %w", err)
	}
	fmt.Fprintln(w.out, "Connection and selected plugins verified. Claude account login is not checked by this remote health probe. Use shiki in your project directory to create or select a project and session; file synchronization still requires your explicit mapping and conflict choices.")
	return nil
}

func (w *setupWizard) node(ctx context.Context) error {
	status := new(apb.NodeStatus)
	if err := w.backend.query(ctx, status, "node", "status"); err != nil {
		return err
	}
	if status.State == "online" {
		return nil
	}
	if status.State == "offline" && status.ReasonCode == "NODE_CONTROL_UNAVAILABLE" {
		fmt.Fprintln(w.out, "Tailscale control service is temporarily unavailable. Checking the saved server connection; no new device authorization is started.")
		return nil
	}
	if status.ReasonCode == "NODE_DISABLED" {
		return errors.New("embedded node is disabled; configure an explicit TLS connection with shiki connect instead")
	}
	fmt.Fprintln(w.out, "Authorize this device using the login URL below. Setup waits up to five minutes; Ctrl+C cancels the wait.")
	lastURL := ""
	showURL := func() {
		if setupLoginActive(status) && status.LoginUrl != "" && status.LoginUrl != lastURL {
			fmt.Fprintln(w.out, "Complete the existing device authorization:", safeText(status.LoginUrl))
			lastURL = status.LoginUrl
		}
	}
	if setupLoginActive(status) {
		// The installer may already own the active login. Its URL and attempt
		// remain usable; creating another attempt would fail LOGIN_IN_PROGRESS.
		showURL()
	} else if err := w.backend.run(ctx, true, "node", "login"); err != nil && connect.CodeOf(err) != connect.CodeAlreadyExists {
		return fmt.Errorf("node login remains pending: %w", err)
	}
	wait, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		if err := w.backend.query(wait, status, "node", "status"); err != nil {
			return err
		}
		if status.State == "online" {
			return nil
		}
		if !setupLoginActive(status) {
			return fmt.Errorf("node authorization remains %s (%s); rerun setup when authorization or device approval is complete", safeText(status.State), safeText(status.ReasonCode))
		}
		showURL()
		select {
		case <-wait.Done():
			return fmt.Errorf("node authorization still pending; rerun setup to continue: %w", wait.Err())
		case <-time.After(time.Second):
		}
	}
}

func setupLoginActive(status *apb.NodeStatus) bool {
	return nodeLoginActive(status) && status.LoginId != "" && status.LoginExpiresAt > time.Now().Unix()
}

// Explicit answers accept the familiar short forms; unrecognized input must not
// silently decline a requested change or accidentally grant it.
func (w *setupWizard) confirm(ctx context.Context, label string, defaultYes bool) (bool, error) {
	fallback := "no"
	if defaultYes {
		fallback = "yes"
	}
	for {
		answer, err := w.prompt(ctx, label+" yes/no", fallback)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		case "":
			return defaultYes, nil
		default:
			fmt.Fprintln(w.out, "Enter yes (y) or no (n).")
		}
	}
}
