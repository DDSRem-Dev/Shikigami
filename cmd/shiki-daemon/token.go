package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	"github.com/spf13/cobra"
)

func openAuth(path string) (*auth.Manager, *config.Daemon, error) {
	cfg, err := config.LoadDaemon(path)
	if err != nil {
		return nil, nil, err
	}
	if err = cfg.CheckRunUser(); err != nil {
		return nil, nil, err
	}
	for _, name := range []string{filepath.Join(cfg.StateDir, "state.json"), filepath.Join(cfg.StateDir, "auth", "state.json")} {
		if _, err = storage.ReadPrivateFile(name); err != nil {
			return nil, nil, fmt.Errorf("daemon identity is not initialized; run init with the same --config: %w", err)
		}
	}
	m, err := auth.Open(filepath.Join(cfg.StateDir, "auth"), cfg.RunUser)
	return m, cfg, err
}

func tokenCommand(configPath *string) *cobra.Command {
	root := &cobra.Command{Use: "token", Short: "Manage device credentials locally as the daemon user"}
	root.AddCommand(&cobra.Command{Use: "list", Short: "List token IDs and claims without secrets", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		m, _, err := openAuth(*configPath)
		if err != nil {
			return err
		}
		defer m.Close()
		tokens, err := m.List()
		if err != nil {
			return err
		}
		if tokens == nil {
			tokens = []auth.TokenInfo{}
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(tokens)
	}})
	root.AddCommand(&cobra.Command{Use: "revoke <token-id>", Short: "Revoke a token and cancel its active RPCs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		m, _, err := openAuth(*configPath)
		if err != nil {
			return err
		}
		defer m.Close()
		return m.Revoke(args[0])
	}})
	for _, refresh := range []bool{false, true} {
		var output, runUser string
		var expires time.Duration
		var permissions []string
		name, arguments := "create", cobra.NoArgs
		if refresh {
			name, arguments = "refresh <token-id>", cobra.ExactArgs(1)
		}
		cmd := &cobra.Command{Use: name, Short: "Issue a credential once to stdout or a new private --output file", Args: arguments, RunE: func(cmd *cobra.Command, args []string) error {
			m, cfg, err := openAuth(*configPath)
			if err != nil {
				return err
			}
			defer m.Close()
			if runUser != "" && runUser != cfg.RunUser {
				return errors.New("--user must match daemon run_user")
			}
			destination, err := newTokenDestination(output, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			defer destination.abort()
			var token string
			var info auth.TokenInfo
			if refresh {
				token, info, err = m.Refresh(args[0], expires)
			} else {
				token, info, err = m.Create(cfg.RunUser, permissions, expires)
			}
			if err != nil {
				return err
			}
			if err = destination.write(token); err != nil {
				return errors.Join(errors.New("credential output failed; issued credential revoked"), m.Revoke(info.ID), err)
			}
			return nil
		}}
		cmd.Flags().StringVar(&output, "output", "", "Write credential to a new mode-0600 file instead of stdout")
		cmd.Flags().DurationVar(&expires, "expires", 720*time.Hour, "Credential lifetime using h/m/s units")
		if !refresh {
			cmd.Flags().StringVar(&runUser, "user", "", "Must match configured run_user")
			cmd.Flags().StringSliceVar(&permissions, "permissions", []string{"*"}, "Comma-separated explicit permissions or namespace wildcards")
		}
		root.AddCommand(cmd)
	}
	return root
}

type tokenDestination struct {
	writer    io.Writer
	file      *os.File
	path      string
	committed bool
}

func newTokenDestination(path string, stdout io.Writer) (*tokenDestination, error) {
	if path == "" {
		return &tokenDestination{writer: stdout}, nil
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("--output must be an absolute path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	return &tokenDestination{writer: file, file: file, path: path}, nil
}
func (d *tokenDestination) write(token string) error {
	if _, err := fmt.Fprintln(d.writer, token); err != nil {
		return err
	}
	if d.file != nil {
		err := d.file.Sync()
		err = errors.Join(err, d.file.Close())
		d.file = nil
		if err != nil {
			return err
		}
		dir, err := os.Open(filepath.Dir(d.path))
		if err != nil {
			return err
		}
		err = errors.Join(dir.Sync(), dir.Close())
		if err != nil {
			return err
		}
	}
	d.committed = true
	return nil
}
func (d *tokenDestination) abort() {
	if d.file != nil {
		_ = d.file.Close()
	}
	if !d.committed && d.path != "" {
		_ = os.Remove(d.path)
	}
}
