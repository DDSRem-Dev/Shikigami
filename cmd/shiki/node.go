package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/spf13/cobra"
)

func (a *app) nodeCommands() *cobra.Command {
	var socket string
	root := &cobra.Command{Use: "node", Short: "Log in or inspect the embedded node through local-only control", Args: cobra.NoArgs}
	root.PersistentFlags().StringVar(&socket, "socket", "", "Local agent or daemon control socket; defaults to agent.socket")
	socketPath := func() (string, error) {
		if socket == "" {
			_, cfg, e := client.LoadConfig(a.configPath)
			if e != nil {
				return "", e
			}
			return cfg.Agent.Socket, nil
		}
		return socket, nil
	}
	withNode := func(ctx context.Context, run func(*client.NodeClient) error) error {
		path, e := socketPath()
		if e != nil {
			return e
		}
		node, e := client.OpenNode(ctx, path, a.timeout)
		if e != nil {
			return e
		}
		defer node.Close()
		return run(node)
	}
	status := &cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		path, e := socketPath()
		if e != nil {
			return e
		}
		result, e := client.GetNodeStatus(cmd.Context(), path, a.timeout)
		if e != nil {
			return e
		}
		return a.print(result)
	}}
	root.RunE = status.RunE
	root.AddCommand(status)
	var keyFile string
	var wait time.Duration
	login := &cobra.Command{Use: "login", Args: cobra.NoArgs, Short: "Begin explicit login; show the local login URL or use a private auth-key file", RunE: func(cmd *cobra.Command, _ []string) error {
		if wait <= 0 {
			return errors.New("--wait-timeout must be positive")
		}
		request := &apb.BeginLoginRequest{Method: &apb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: a.id()}
		if keyFile != "" {
			key, e := readPrivateSecret(keyFile, 4096)
			if e != nil {
				return e
			}
			defer clear(key)
			request.Method = &apb.BeginLoginRequest_AuthKey{AuthKey: key}
		}
		return withNode(cmd.Context(), func(node *client.NodeClient) error {
			result, e := node.API().BeginLogin(cmd.Context(), request)
			if e != nil {
				return e
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), wait)
			defer cancel()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for nodeLoginActive(result) && result.LoginUrl == "" {
				select {
				case <-ctx.Done():
					if e = a.print(result); e != nil {
						return e
					}
					return fmt.Errorf("login accepted; inspect node status for completion: %w", ctx.Err())
				case <-ticker.C:
				}
				result, e = node.API().GetStatus(ctx, &pb.Empty{})
				if e != nil {
					return e
				}
			}
			if e = a.print(result); e != nil {
				return e
			}
			if result.State != "online" && result.LoginUrl == "" {
				return fmt.Errorf("node login %s: %s", result.State, safeText(result.ReasonCode))
			}
			return nil
		})
	}}
	login.Flags().StringVar(&keyFile, "auth-key-file", "", "Owned regular 0600 file; key is never passed in argv or persisted")
	login.Flags().DurationVar(&wait, "wait-timeout", 15*time.Second, "Wait for a login URL or terminal login result")
	root.AddCommand(login)
	root.AddCommand(&cobra.Command{Use: "cancel <login-id>", Args: cobra.ExactArgs(1), Short: "Cancel exactly the selected login attempt", RunE: func(cmd *cobra.Command, args []string) error {
		return withNode(cmd.Context(), func(node *client.NodeClient) error {
			result, e := node.API().CancelLogin(cmd.Context(), &apb.CancelLoginRequest{LoginId: args[0]})
			if e != nil {
				return e
			}
			return a.print(result)
		})
	}})
	return root
}

// NeedsLogin may be published while an accepted asynchronous login is still
// obtaining its URL or authenticating an auth key. Only the active, unexpired
// attempt is pending; cancellation, expiry and explicit rejection stay terminal.
func nodeLoginActive(status *apb.NodeStatus) bool {
	return status.State == "authenticating" ||
		(status.State == "needs_login" && status.ReasonCode == "NODE_AUTH_REQUIRED" &&
			status.LoginId != "" && status.LoginExpiresAt > time.Now().Unix())
}

func readPrivateSecret(path string, limit int64) ([]byte, error) {
	path, e := client.ConfigPath(path)
	if e != nil {
		return nil, e
	}
	file, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		return nil, e
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errors.New("secret file must be an owned regular file with mode 0600")
	}
	data, e := io.ReadAll(io.LimitReader(file, limit+1))
	if e != nil {
		return nil, e
	}
	defer clear(data)
	if int64(len(data)) > limit {
		return nil, errors.New("secret file exceeds the allowed size")
	}
	value := strings.TrimSpace(string(data))
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return nil, errors.New("secret file must contain one nonempty value")
	}
	return []byte(value), nil
}
