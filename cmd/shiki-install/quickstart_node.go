package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
)

type quickstartNodeStatus struct {
	State     string   `json:"state"`
	Reason    string   `json:"reason_code"`
	URL       string   `json:"login_url"`
	Addresses []string `json:"addresses"`
	LoginID   string   `json:"login_id"`
	Expires   int64    `json:"login_expires_at,string"`
}

func quickstartNode(parent context.Context, plan install.Plan, prefix string, tty *os.File) ([]string, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	cli := filepath.Join(prefix, "current", "shiki")
	base := []string{"--json", "node", "--socket=" + filepath.Join(plan.StateDir, "run", "admin.sock")}
	inspect := func() (quickstartNodeStatus, error) {
		data, err := quickstartCapture(ctx, plan, cli, append(base, "status")...)
		var status quickstartNodeStatus
		if err != nil {
			return status, err
		}
		err = json.Unmarshal(data, &status)
		return status, err
	}
	begin := func() error {
		_, err := quickstartCapture(ctx, plan, cli, append(base, "login")...)
		return err
	}
	return quickstartWaitNode(ctx, inspect, begin, tty, 2*time.Second)
}

func quickstartLoginActive(status quickstartNodeStatus, now int64) bool {
	if status.LoginID == "" || status.Expires <= now {
		return false
	}
	switch status.State {
	case "authenticating":
		return true
	case "uninitialized":
		return status.Reason == ""
	case "needs_login":
		return status.Reason == "NODE_AUTH_REQUIRED"
	case "offline":
		return status.Reason == "NODE_CONTROL_UNAVAILABLE"
	case "denied":
		return status.Reason == "NODE_APPROVAL_REQUIRED"
	case "expired":
		return status.Reason == "NODE_KEY_EXPIRED"
	}
	return false
}

func quickstartWaitNode(ctx context.Context, inspect func() (quickstartNodeStatus, error), begin func() error, tty io.Writer, interval time.Duration) ([]string, error) {
	loginStarted, lastURL, lastState := false, "", ""
	observedActive := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		status, err := inspect()
		if err != nil {
			return nil, fmt.Errorf("cannot inspect the installed node: %w", err)
		}
		if status.State == "online" {
			if len(status.Addresses) == 0 {
				return nil, errors.New("online node has not reported an address")
			}
			return status.Addresses, nil
		}
		active := quickstartLoginActive(status, time.Now().Unix())
		if active {
			observedActive = true
		}
		if active && status.URL != "" && status.URL != lastURL {
			fmt.Fprintln(tty, "Open this private Tailscale login URL on your Mac:", status.URL)
			lastURL = status.URL
		}
		if status.State != lastState {
			fmt.Fprintf(tty, "Waiting for node authorization: %s (%s). Ctrl+C leaves the installation available for resume.\n", status.State, status.Reason)
			lastState = status.State
		}
		if !active && (loginStarted || observedActive) {
			return nil, fmt.Errorf("node authorization ended or expired (%s / %s); rerun to continue", status.State, status.Reason)
		}
		if !loginStarted && !active && (status.State == "uninitialized" || status.State == "needs_login" || status.State == "expired") {
			loginStarted = true
			if callErr := begin(); callErr != nil {
				// Another frontend may have won BeginLogin; re-read its live
				// attempt instead of treating AlreadyExists as a fatal failure.
				next, inspectErr := inspect()
				if inspectErr != nil || (!quickstartLoginActive(next, time.Now().Unix()) && next.State != "online") {
					return nil, fmt.Errorf("node login could not start: %w", callErr)
				}
			}
			continue
		}
		if !active {
			return nil, fmt.Errorf("node is %s (%s)", status.State, status.Reason)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}
