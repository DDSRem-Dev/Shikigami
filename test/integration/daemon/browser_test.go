//go:build linux

package daemon_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// This exercises the installed subprocess, real authentication and persistent
// profile/file behavior. Chrome rendering is separately exercised by the
// disposable systemd/browser acceptance environment, not simulated here.
func TestBrowserProfileAuthorizationAndPersistence(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("daemon integration requires an ordinary user")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	root, err := os.MkdirTemp("/tmp", "shiki-br-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	artifacts := filepath.Join(root, "plugins")
	if err = os.Mkdir(artifacts, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(artifacts, "shiki-plugin-browser")
	build := exec.CommandContext(t.Context(), "go", "build", "-race", "-o", binary, "./plugins/browser")
	build.Dir = filepath.Join("..", "..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build browser: %v\n%s", err, output)
	}
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	contract, err := sdk.BuiltinProtocol("browser", "both")
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := host.ReadPluginManifest(filepath.Join("..", "..", "..", "plugins", "browser"))
	if err != nil {
		t.Fatal(err)
	}
	declaration.Version = buildinfo.Version
	manifest := host.Manifest{Name: "browser", Version: buildinfo.Version, Executable: "shiki-plugin-browser", SHA256: hex.EncodeToString(digest[:]), Side: "both", Protocol: contract, PluginManifest: declaration}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(artifacts, "browser.manifest.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}

	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := certServer.TLS.Certificates[0]
	certServer.Close()
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	cfg := config.DefaultDaemon(account.Username, filepath.Join(root, "state"))
	cfg.ProjectsDir = filepath.Join(root, "projects")
	cfg.PluginsDir = artifacts
	cfg.Tailscale.Enabled = false
	cfg.Claude.Executable = "/bin/sh"
	cfg.Server.DirectTLS = config.DirectTLS{Enabled: true, Address: address, CertFile: certPath, KeyFile: keyPath}
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Second)
	defer cancel()
	daemon, err := server.StartDaemon(ctx, cfg, server.DaemonOptions{Build: buildinfo.Version})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if daemon != nil {
			_ = daemon.Close()
		}
	}()
	token, _, err := daemon.Service.Auth.Create(account.Username, []string{"*"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	open := func(token string) (rpc.ShikiDaemonClient, erpc.BrowserServiceClient, *connection.Endpoint) {
		t.Helper()
		endpoint, err := connection.NewTLSClient("https://"+address, connection.TLSOptions{RootCAs: roots})
		if err != nil {
			t.Fatal(err)
		}
		credential, err := connection.BearerToken(token)
		if err != nil {
			t.Fatal(err)
		}
		client := connection.NewRPCClient(endpoint, credential)
		return rpc.NewShikiDaemonClient(client), erpc.NewBrowserServiceClient(client), endpoint
	}
	core, browser, endpoint := open(token)
	defer endpoint.Close()
	if _, err = browser.ListBrowserProfiles(ctx, &epb.BrowserScope{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("disabled browser did not fail closed: %v", err)
	}
	offer := &pb.ProtocolOffer{Major: 1, Capabilities: []string{sdk.BrowserCapability, sdk.BrowserLaunchModeCapability}}
	beforeEnable, err := core.NegotiateProtocol(ctx, offer)
	if err != nil || slices.Contains(beforeEnable.GetCapabilities(), sdk.BrowserLaunchModeCapability) {
		t.Fatal("disabled worker advertised mode support", beforeEnable, err)
	}
	status := daemonPluginStatus(t, ctx, core, "browser")
	if _, err = core.ManagePlugin(ctx, &pb.ManagePluginRequest{PluginId: "browser", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: status.Revision, RequestId: "enable-browser"}); err != nil {
		t.Fatal(err)
	}
	awaitDaemonPlugin(t, ctx, core, "browser", "ready")
	afterEnable, err := core.NegotiateProtocol(ctx, offer)
	if err != nil || !slices.Contains(afterEnable.GetCapabilities(), sdk.BrowserLaunchModeCapability) {
		t.Fatal("ready mode-aware worker capability missing", afterEnable, err)
	}
	create := &epb.PutBrowserProfileRequest{Profile: &epb.BrowserProfile{Name: "work", Region: &epb.RegionConfig{Country: "JP", Timezone: "Asia/Tokyo", Locale: "ja_JP.UTF-8", Languages: []string{"ja-JP"}}}, RequestId: "create-work"}
	profile, err := browser.PutBrowserProfile(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Id == "" || profile.Revision == "" {
		t.Fatalf("missing persistent profile identity: %v", profile)
	}
	replayed, err := browser.PutBrowserProfile(ctx, create)
	if err != nil || replayed.GetId() != profile.Id || replayed.GetRevision() != profile.Revision {
		t.Fatalf("profile create replay changed resource: %v %v", replayed, err)
	}
	if _, err = browser.PutBrowserProfile(ctx, &epb.PutBrowserProfileRequest{Profile: &epb.BrowserProfile{Id: profile.Id, Name: "renamed", Region: profile.Region}, ExpectedRevision: "stale", RequestId: "stale-update"}); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale profile edit accepted: %v", err)
	}
	if _, err = browser.DeleteBrowserProfile(ctx, &epb.DeleteBrowserProfileRequest{ProfileId: profile.Id, ExpectedRevision: profile.Revision, RequestId: "unconfirmed-delete"}); err == nil {
		t.Fatal("unconfirmed profile deletion succeeded")
	}
	readToken, _, err := daemon.Service.Auth.Create(account.Username, []string{"browser.list", "browser.files.read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, reader, readEndpoint := open(readToken)
	defer readEndpoint.Close()
	if _, err = reader.PutBrowserProfile(ctx, &epb.PutBrowserProfileRequest{Profile: &epb.BrowserProfile{Name: "unauthorized"}, RequestId: "denied-create"}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("read token created profile: %v", err)
	}

	// Keep the request side open: permission and worker validation failures
	// must reach an idle sender without waiting for an upload half-close.
	for _, test := range []struct {
		name, token string
		first       *epb.BrowserFileChunk
		code        connect.Code
	}{
		{"read-only-token", readToken, &epb.BrowserFileChunk{ProfileId: profile.Id, Name: "denied.bin", TotalSize: 1, RequestId: "denied-upload"}, connect.CodePermissionDenied},
		{"worker-invalid-name", token, &epb.BrowserFileChunk{ProfileId: profile.Id, Name: "../cookies", TotalSize: 1, RequestId: "invalid-upload"}, connect.CodeInvalidArgument},
	} {
		t.Run(test.name, func(t *testing.T) {
			call, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			credential, err := connection.BearerToken(test.token)
			if err != nil {
				t.Fatal(err)
			}
			raw := connection.NewRPCClient(endpoint, credential)
			stream, err := raw.CallClientStream(call, connect.Spec{StreamType: connect.StreamTypeClient, Schema: epb.File_extensions_proto.Services().ByName("BrowserService").Methods().ByName("WriteBrowserFile"), Procedure: erpc.BrowserServiceWriteBrowserFileProcedure})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			sendErr := stream.Send(test.first)
			if err := stream.Receive(new(epb.BrowserFile)); connect.CodeOf(err) != test.code {
				t.Fatalf("idle upload result: %v (send: %v); want %v", err, sendErr, test.code)
			}
		})
	}

	t.Run("partial-upload-cancellation", func(t *testing.T) {
		call, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		stream, err := browser.WriteBrowserFile(call)
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.Send(&epb.BrowserFileChunk{ProfileId: profile.Id, Name: "canceled.bin", TotalSize: 2, RequestId: "canceled-upload", Data: []byte{1}}); err != nil {
			t.Fatal(err)
		}
		stage := filepath.Join(cfg.StateDir, "plugin-data", "browser", "profiles", profile.Id, "uploads")
		temporary := filepath.Join(stage, ".upload-*")
		for {
			paths, err := filepath.Glob(temporary)
			if err != nil {
				t.Fatal(err)
			}
			if len(paths) == 1 {
				break
			}
			select {
			case <-call.Done():
				t.Fatal("partial upload did not reach the actual browser subprocess")
			case <-time.After(5 * time.Millisecond):
			}
		}
		cancel()
		if _, err := stream.CloseAndReceive(); connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatalf("upload cancellation was not terminal: %v", err)
		}
		drained, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		for {
			paths, err := filepath.Glob(temporary)
			if err != nil {
				t.Fatal(err)
			}
			if len(paths) == 0 {
				break
			}
			select {
			case <-drained.Done():
				t.Fatal("canceled upload retained private temporary state")
			case <-time.After(5 * time.Millisecond):
			}
		}
		if _, err := os.Stat(filepath.Join(stage, "canceled.bin")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canceled upload published a file: %v", err)
		}
	})

	payload := []byte("private upload through TLS and the real plugin\x00\xff\n")
	sum := sha256.Sum256(payload)
	uploadCtx, cancelUpload := context.WithCancel(ctx)
	defer cancelUpload()
	upload, err := browser.WriteBrowserFile(uploadCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err = upload.Send(&epb.BrowserFileChunk{ProfileId: profile.Id, Name: "sample.bin", TotalSize: int64(len(payload)), RequestId: "upload-sample", Sha256: hex.EncodeToString(sum[:])}); err != nil {
		t.Fatal(err)
	}
	if err = upload.Send(&epb.BrowserFileChunk{Data: payload}); err != nil {
		t.Fatal(err)
	}
	if err = upload.Send(&epb.BrowserFileChunk{Finish: true}); err != nil {
		t.Fatal(err)
	}
	stored, err := upload.CloseAndReceive()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Size != int64(len(payload)) || stored.Sha256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("upload receipt mismatch: %v", stored)
	}
	escape, escapeErr := reader.ReadBrowserFile(ctx, &epb.BrowserFileRequest{ProfileId: profile.Id, Area: "uploads", Name: "../cookies"})
	if escapeErr == nil {
		_, escapeErr = escape.Receive()
		_ = escape.Close()
	}
	if escapeErr == nil || errors.Is(escapeErr, io.EOF) {
		t.Fatal("browser file traversal accepted")
	}
	read, err := reader.ReadBrowserFile(ctx, &epb.BrowserFileRequest{ProfileId: profile.Id, Area: "uploads", Name: "sample.bin"})
	if err != nil {
		t.Fatal(err)
	}
	var received bytes.Buffer
	for {
		chunk, err := read.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = read.Close()
			t.Fatal(err)
		}
		received.Write(chunk.Data)
	}
	_ = read.Close()
	if !bytes.Equal(payload, received.Bytes()) {
		t.Fatal("browser upload contents changed")
	}

	if err = daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon = nil
	endpoint.Close()
	readEndpoint.Close()
	daemon, err = server.StartDaemon(ctx, cfg, server.DaemonOptions{Build: buildinfo.Version})
	if err != nil {
		t.Fatal(err)
	}
	core, browser, endpoint = open(token)
	defer endpoint.Close()
	awaitDaemonPlugin(t, ctx, core, "browser", "ready")
	profiles, err := browser.ListBrowserProfiles(ctx, &epb.BrowserScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles.Profiles) != 1 || profiles.Profiles[0].Id != profile.Id || profiles.Profiles[0].Revision != profile.Revision {
		t.Fatalf("profile did not survive daemon restart: %v", profiles)
	}
	files, err := browser.ListBrowserFiles(ctx, &epb.BrowserFileRequest{ProfileId: profile.Id, Area: "uploads"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Files) != 1 || files.Files[0].Name != "sample.bin" || files.Files[0].Size != int64(len(payload)) {
		t.Fatalf("upload did not survive restart: %v", files)
	}
	status = daemonPluginStatus(t, ctx, core, "browser")
	if _, err = core.ManagePlugin(ctx, &pb.ManagePluginRequest{PluginId: "browser", Action: pb.ManagePluginRequest_DISABLE, ExpectedRevision: status.Revision, RequestId: "disable-browser"}); err != nil {
		t.Fatal(err)
	}
	awaitDaemonPlugin(t, ctx, core, "browser", "stopped")
	if _, err = browser.ListBrowserProfiles(ctx, &epb.BrowserScope{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("disabled browser continued serving profiles: %v", err)
	}
}
