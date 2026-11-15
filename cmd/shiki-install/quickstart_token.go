package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/install"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

// This private helper is invoked after dropping to the daemon UID. Only a
// boolean leaves it: signature, issuer, daemon identity, expiry and revocation
// are checked using the same auth.Manager as normal daemon admission.
func quickstartTokenCheck(args []string, out io.Writer) (int, error) {
	f := flag.NewFlagSet("private token check", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var configPath, tokenPath string
	f.StringVar(&configPath, "config", "", "")
	f.StringVar(&tokenPath, "token-file", "", "")
	if err := f.Parse(args); err != nil {
		return 2, err
	}
	if f.NArg() != 0 || !filepath.IsAbs(configPath) || !filepath.IsAbs(tokenPath) || os.Geteuid() == 0 {
		return 2, errors.New("private token check requires the configured ordinary UID and absolute paths")
	}
	cfg, err := config.LoadDaemon(configPath)
	if err != nil {
		return 1, err
	}
	if err = cfg.CheckRunUser(); err != nil {
		return 1, err
	}
	// Never initialize a replacement identity just to check a credential.
	for _, path := range []string{filepath.Join(cfg.StateDir, "state.json"), filepath.Join(cfg.StateDir, "auth", "state.json")} {
		data, err := quickstartPrivateRead(path, uint32(os.Geteuid()), 64<<20)
		clear(data)
		if err != nil {
			return 1, err
		}
	}
	token, err := quickstartPrivateRead(tokenPath, uint32(os.Geteuid()), 16384)
	if err != nil {
		return 1, err
	}
	defer clear(token)
	manager, err := auth.Open(filepath.Join(cfg.StateDir, "auth"), cfg.RunUser)
	if err != nil {
		return 1, err
	}
	defer manager.Close()
	_, verifyErr := manager.Verify(string(bytes.TrimSpace(token)))
	err = json.NewEncoder(out).Encode(struct {
		Valid bool `json:"valid"`
	}{verifyErr == nil})
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func quickstartPairingToken(ctx context.Context, plan install.Plan, prefix, base string, uid uint32, tty *os.File) ([]byte, string, error) {
	path := base
	for index := 0; index < 64; index++ {
		token, err := quickstartPrivateRead(path, uid, 16384)
		if errors.Is(err, os.ErrNotExist) {
			if index != 0 {
				fmt.Fprint(tty, "The saved connection credential is expired, revoked or invalid. Create a replacement for your Mac and retain the old file? [y/N] ")
				answer, err := client.ReadConsoleLine(ctx, tty, 32)
				if err != nil {
					return nil, "", err
				}
				if strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes" {
					return nil, "", errors.New("credential renewal deferred; rerun quickstart when ready")
				}
			}
			if err = quickstartCommand(ctx, plan, true, tty, nil, filepath.Join(prefix, "current", "shiki-daemon"), "--config="+plan.Config, "token", "create", "--output="+path); err != nil {
				return nil, "", err
			}
			token, err = quickstartPrivateRead(path, uid, 16384)
		}
		if err != nil {
			return nil, "", err
		}
		data, err := quickstartCapture(ctx, plan, filepath.Join(prefix, "current", "shiki-install"), "_quickstart-token-check", "--config="+plan.Config, "--token-file="+path)
		if err != nil {
			clear(token)
			return nil, "", errors.New("could not verify the saved credential against the daemon identity; preserved for review")
		}
		var result struct {
			Valid bool `json:"valid"`
		}
		if err = json.Unmarshal(data, &result); err != nil {
			clear(token)
			return nil, "", err
		}
		if result.Valid {
			return token, path, nil
		}
		// A deterministic successor makes a lost reply/retry reuse the same
		// already issued replacement. Never overwrite or delete old credentials.
		digest := sha256.Sum256(token)
		clear(token)
		path = base + ".renewed-" + hex.EncodeToString(digest[:8])
	}
	return nil, "", errors.New("credential recovery chain exceeds its bound; retained for administrator review")
}
