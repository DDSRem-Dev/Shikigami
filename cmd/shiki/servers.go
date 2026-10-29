package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

type serverFlags struct {
	tlsAddress, tlsName, ca, tailnet, daemonID, tokenFile string
	tokenStdin                                            bool
}

func serverOptions(cmd *cobra.Command, values *serverFlags) {
	cmd.Flags().StringVar(&values.tlsAddress, "tls-address", "", "Explicit direct TLS host:port")
	cmd.Flags().StringVar(&values.tlsName, "tls-server-name", "", "Expected TLS certificate name (defaults to address host)")
	cmd.Flags().StringVar(&values.ca, "tls-ca", "", "Trusted PEM CA file; empty uses system roots")
	cmd.Flags().StringVar(&values.tailnet, "tailscale-address", "", "Tailnet peer address; omitted port defaults to 9527")
	cmd.Flags().StringVar(&values.daemonID, "daemon-id", "", "Previously verified daemon identity")
	cmd.Flags().StringVar(&values.tokenFile, "token-file", "", "Read the credential from an owned 0600 file")
	cmd.Flags().BoolVar(&values.tokenStdin, "token-stdin", false, "Explicitly read the credential from stdin until EOF")
	cmd.MarkFlagsMutuallyExclusive("token-file", "token-stdin")
}
func (a *app) connectCommand() *cobra.Command {
	flags := new(serverFlags)
	command := &cobra.Command{Use: "connect [name]", Short: "Configure a server, store its credential, and verify the connection", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error { return a.saveServer(cmd, c, args, flags, false, true) })
	}}
	serverOptions(command, flags)
	return command
}
func (a *app) serverCommands() *cobra.Command {
	root := &cobra.Command{Use: "servers", Short: "Manage server records through the agent"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			records, e := c.Agent().ListServers(cmd.Context(), &pb.Empty{})
			if e != nil {
				return e
			}
			return a.print(records)
		})
	}}
	root.RunE = list.RunE
	root.Args = cobra.NoArgs
	root.AddCommand(list)
	for _, update := range []bool{false, true} {
		flags := new(serverFlags)
		name := "add"
		if update {
			name = "update"
		}
		command := &cobra.Command{Use: name + " [name-or-id]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return a.withClient(cmd.Context(), true, func(c *client.Client) error { return a.saveServer(cmd, c, args, flags, update, false) })
		}}
		serverOptions(command, flags)
		root.AddCommand(command)
	}
	var yes bool
	remove := &cobra.Command{Use: "remove <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			record, e := c.ResolveServer(cmd.Context(), args[0])
			if e != nil {
				return e
			}
			if e = a.confirm(cmd.Context(), "Remove server "+safeText(record.Config.Name), yes); e != nil {
				return e
			}
			result, e := c.Agent().RemoveServer(cmd.Context(), &apb.RemoveServerRequest{ServerId: record.Config.Id, ExpectedRevision: record.Revision, RequestId: a.id()})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}}
	remove.Flags().BoolVar(&yes, "yes", false, "Confirm removal without prompting")
	root.AddCommand(remove)
	root.AddCommand(&cobra.Command{Use: "switch <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			record, e := c.ResolveServer(cmd.Context(), args[0])
			if e != nil {
				return e
			}
			preferences, e := c.Agent().GetPreferences(cmd.Context(), &pb.Empty{})
			if e != nil {
				return e
			}
			preferences.DefaultServerId = record.Config.Id
			saved, e := c.Agent().SetPreferences(cmd.Context(), &apb.SetPreferencesRequest{Values: preferences, RequestId: a.id()})
			if e != nil {
				return e
			}
			return a.print(saved)
		})
	}})
	return root
}
func (a *app) saveServer(cmd *cobra.Command, c *client.Client, args []string, flags *serverFlags, update, probe bool) error {
	ctx := cmd.Context()
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	var e error
	if name == "" {
		name, e = a.prompt(ctx, "Server name", "")
		if e != nil {
			return e
		}
	}
	if name == "" {
		return errors.New("server name is required")
	}
	// Connecting a known server renews or verifies that record. Only explicitly
	// supplied flags replace its connection settings.
	if probe && !update {
		servers, e := c.Agent().ListServers(ctx, &pb.Empty{})
		if e != nil {
			return e
		}
		for _, record := range servers.Servers {
			if record.Config != nil && (record.Config.Id == name || record.Config.Name == name) {
				update = true
				break
			}
		}
	}
	config := &apb.ServerConfig{Name: name}
	expected, credentialRevision := "", ""
	credentialPresent := false
	if update {
		record, e := c.ResolveServer(ctx, name)
		if e != nil {
			return e
		}
		config = proto.Clone(record.Config).(*apb.ServerConfig)
		expected, credentialRevision = record.Revision, record.CredentialRevision
		credentialPresent = record.CredentialPresent
	}
	if !update || cmd.Flags().Changed("tls-address") {
		config.DirectTlsAddress = flags.tlsAddress
	}
	if !update || cmd.Flags().Changed("tailscale-address") {
		config.TailscaleAddress = flags.tailnet
	}
	if config.DirectTlsAddress == "" && config.TailscaleAddress == "" {
		if !c.Config.Tailscale.Enabled {
			return errors.New("tailnet is disabled; provide an explicit --tls-address and TLS trust settings")
		}
		config.TailscaleAddress, e = a.prompt(ctx, "Server address (Tailscale IP or hostname; default port 9527)", "")
		if e != nil {
			return e
		}
	}
	if config.TailscaleAddress != "" {
		if !c.Config.Tailscale.Enabled {
			return errors.New("--tailscale-address requires tailscale.enabled in client configuration")
		}
		config.TailscaleAddress, e = normalizeTailnetAddress(config.TailscaleAddress)
		if e != nil {
			return e
		}
	}
	if !update || cmd.Flags().Changed("tls-server-name") {
		config.TlsServerName = flags.tlsName
	}
	if config.DirectTlsAddress != "" && config.TlsServerName == "" {
		host, _, e := net.SplitHostPort(config.DirectTlsAddress)
		if e != nil {
			return errors.New("--tls-address must be host:port")
		}
		config.TlsServerName = host
	}
	if !update || cmd.Flags().Changed("tls-ca") {
		config.TlsCaFile = flags.ca
	}
	if config.TlsCaFile != "" {
		config.TlsCaFile, e = client.ConfigPath(config.TlsCaFile)
		if e != nil {
			return e
		}
	}
	if !update || cmd.Flags().Changed("daemon-id") {
		config.DaemonId = flags.daemonID
	}
	token, e := a.credential(ctx, flags, probe && !credentialPresent)
	if e != nil {
		return e
	}
	defer clear(token)
	record, e := c.Agent().UpsertServer(ctx, &apb.UpsertServerRequest{Config: config, ExpectedRevision: expected, RequestId: a.id()})
	if e != nil {
		return e
	}
	if len(token) > 0 {
		if !update {
			credentialRevision = record.CredentialRevision
		}
		record, e = c.Agent().SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: record.Config.Id, Token: token, ExpectedCredentialRevision: credentialRevision, RequestId: a.id() + "-credential"})
		if e != nil {
			return e
		}
	}
	if probe {
		if config.TailscaleAddress != "" {
			if e = a.prepareTailnetLogin(ctx, c); e != nil {
				return e
			}
		}
		record, e = awaitServerReady(ctx, c.Agent(), record)
		if e != nil {
			return fmt.Errorf("server saved, but connection verification failed: %w", e)
		}
		preferences, e := c.Agent().GetPreferences(ctx, &pb.Empty{})
		if e != nil {
			return e
		}
		preferences.DefaultServerId = record.Config.Id
		if _, e = c.Agent().SetPreferences(ctx, &apb.SetPreferencesRequest{Values: preferences, RequestId: a.id() + "-default"}); e != nil {
			return e
		}
		record, e = c.ResolveServer(ctx, record.Config.Id)
		if e != nil {
			return e
		}
	}
	return a.print(record)
}
func (a *app) credential(ctx context.Context, flags *serverFlags, required bool) ([]byte, error) {
	var data []byte
	var e error
	switch {
	case flags.tokenFile != "":
		path, e := client.ConfigPath(flags.tokenFile)
		if e != nil {
			return nil, e
		}
		file, e := os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if e != nil {
			return nil, e
		}
		defer file.Close()
		info, e := file.Stat()
		if e != nil {
			return nil, e
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || owner.Uid != uint32(os.Getuid()) {
			return nil, errors.New("token file must be an owned regular file with mode 0600")
		}
		data, e = io.ReadAll(io.LimitReader(file, 16385))
		if e != nil {
			return nil, e
		}
	case flags.tokenStdin:
		data, e = io.ReadAll(io.LimitReader(a.input, 16385))
		if e != nil {
			return nil, e
		}
	case required:
		if !a.interactive() {
			return nil, errors.New("connect requires --token-file or explicit --token-stdin in non-interactive mode")
		}
		fmt.Fprint(a.errorOutput, "Connection token (hidden): ")
		data, e = client.ReadSecret(ctx, a.input, 16384)
		fmt.Fprintln(a.errorOutput)
		if e != nil {
			return nil, e
		}
	default:
		return nil, nil
	}
	clean := strings.TrimSpace(string(data))
	clear(data)
	if len(clean) > 16384 || !strings.HasPrefix(clean, "shiki_") || strings.ContainsAny(clean, " \t\r\n") {
		return nil, errors.New("invalid connection token format")
	}
	return []byte(clean), nil
}

func normalizeTailnetAddress(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "/\\@ \t\r\n") {
		return "", errors.New("tailnet address must be an IP or hostname with optional port")
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		candidate := strings.Trim(value, "[]")
		if net.ParseIP(candidate) != nil || !strings.ContainsAny(value, ":[]") {
			host, port = candidate, "9527"
		} else {
			return "", errors.New("invalid tailnet host:port")
		}
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || host == "" {
		return "", errors.New("tailnet address requires a host and port between 1 and 65535")
	}
	return net.JoinHostPort(host, port), nil
}
func (a *app) prepareTailnetLogin(ctx context.Context, c *client.Client) error {
	node, err := client.OpenNode(ctx, c.Config.Agent.Socket, a.timeout)
	if err != nil {
		return err
	}
	defer node.Close()
	status, err := node.API().GetStatus(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	if status.State == "online" {
		return nil
	}
	if !a.interactive() {
		return errors.New("server saved; embedded node is not online: run shiki node login, then retry connect")
	}
	if status.State != "authenticating" {
		status, err = node.API().BeginLogin(ctx, &apb.BeginLoginRequest{Method: &apb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: a.id() + "-login"})
		if err != nil {
			return err
		}
	}
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for status.State == "authenticating" && status.LoginUrl == "" {
		select {
		case <-deadline.Done():
			return errors.New("server saved; login is pending: inspect shiki node status")
		case <-ticker.C:
		}
		status, err = node.API().GetStatus(deadline, &pb.Empty{})
		if err != nil {
			return err
		}
	}
	if status.State == "online" {
		return nil
	}
	if status.LoginUrl != "" {
		fmt.Fprintf(a.errorOutput, "Complete embedded node login: %s\n", safeText(status.LoginUrl))
	}
	return errors.New("server saved; finish node login, then retry connect")
}
