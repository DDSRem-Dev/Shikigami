package main

import (
	"context"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/agent"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestConnectCommandVerifiesRestrictedTokensThroughAgentMetadata(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "shiki-connect-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	authentication, err := auth.Open(filepath.Join(directory, "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	defer authentication.Close()
	service := &server.Service{Auth: authentication, Build: "test"}
	tls := httptest.NewUnstartedServer(service.Handler())
	tls.EnableHTTP2 = true
	tls.StartTLS()
	defer tls.Close()
	ca := filepath.Join(directory, "ca.pem")
	if err = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tls.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultClient()
	cfg.Agent.StateDir, cfg.Agent.Socket = filepath.Join(directory, "agent"), filepath.Join(directory, "run", "agent.sock")
	cfg.Tailscale.Enabled, cfg.Agent.AutoStart = false, false
	cfg.PluginsDir = filepath.Join(directory, "plugins")
	configPath := filepath.Join(directory, "client.yaml")
	if err = config.SaveClient(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx, configPath, "test") }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("agent did not stop")
		}
	}()
	var c *client.Client
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); {
		c, err = client.Open(ctx, configPath, client.Options{Timeout: time.Second})
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, test := range []struct {
		name   string
		grants []string
	}{{"stats", []string{"system.stats"}}, {"observer", []string{"session.list", "session.attach"}}} {
		t.Run(test.name, func(t *testing.T) {
			token, _, err := authentication.Create("tester", test.grants, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(directory, test.name+".token")
			if err = os.WriteFile(file, []byte(token), 0600); err != nil {
				t.Fatal(err)
			}
			output, err := runManagement(t, configPath, "connect", test.name, "--tls-address", strings.TrimPrefix(tls.URL, "https://"), "--tls-server-name", "example.com", "--tls-ca", ca, "--token-file", file)
			if err != nil {
				t.Fatal("restricted connection failed", err)
			}
			record := new(apb.ServerRecord)
			if err = protojson.Unmarshal([]byte(output), record); err != nil || record.ConnectionState != "ready" || record.Config.DaemonId == "" || strings.Contains(output, token) {
				t.Fatal("connection did not return a safe verified record", output, err)
			}
			if _, err = c.Remote(record.Config.Id).GetSystemInfo(ctx, &pb.Empty{}); connect.CodeOf(err) != connect.CodePermissionDenied {
				t.Fatal("fixture unexpectedly grants system.info", err)
			}
			if _, err = runManagement(t, configPath, "connect", record.Config.Id); err != nil {
				t.Fatal("explicit reconnection without credential replacement failed", err)
			}
			preferences, err := c.Agent().GetPreferences(ctx, &pb.Empty{})
			if err != nil || preferences.DefaultServerId != record.Config.Id {
				t.Fatal("verified server was not selected", preferences, err)
			}
		})
	}
}
