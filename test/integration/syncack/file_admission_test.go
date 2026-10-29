package syncack_test

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// Exercise production TLS admission and a real sync subprocess.
// Every admitted watch must produce its real initial rescan before saturation
// is asserted; opening a client-side stream is not an admission acknowledgement.
func TestFileWatchAdmissionAcrossConnectionsAndCredentials(t *testing.T) {
	root, binaries := ackArtifacts(t)
	h := startHosts(t, filepath.Join(root, "limits"), binaries, nil)
	for _, side := range []string{"server", "client"} {
		h.manage(side, "sync", pb.ManagePluginRequest_ENABLE, nil)
		h.ready(side)
		h.workerPID(side) // Include both subprocesses in the existing cleanup checks.
	}
	project, err := h.remote.CreateProject(h.ctx, &pb.CreateProjectRequest{Name: "limits", Path: filepath.Join(h.daemonConfig.ProjectsDir, "limits"), RequestId: h.id()})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("metadata remains available while watch slots are full\n")
	if err := os.WriteFile(filepath.Join(project.Path, "file.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	clients := []rpc.ShikiDaemonClient{h.directFileClient([]string{"file.watch"}), h.directFileClient([]string{"file.watch"})}
	var opened []rpc.ShikiDaemonWatchFilesClientStream
	var cancels []context.CancelFunc
	t.Cleanup(func() {
		for _, cancel := range cancels {
			cancel()
		}
		for _, stream := range opened {
			_ = stream.Close()
		}
	})
	// This is the documented shipping ceiling, not a lower injected test limit.
	const watchLimit = 32
	for i := 0; i < watchLimit; i++ {
		ctx, cancel := context.WithCancel(h.ctx)
		cancels = append(cancels, cancel)
		stream, err := clients[i%len(clients)].WatchFiles(ctx, &pb.WatchFilesRequest{ProjectId: project.Id})
		if err != nil {
			t.Fatalf("admit watch %d: %v", i, err)
		}
		opened = append(opened, stream)
		event, err := stream.Receive()
		if err != nil || event.GetType() != pb.FileEvent_RESCAN_REQUIRED || event.GetProjectId() != project.Id {
			t.Fatalf("watch %d not ready: %v %v", i, event, err)
		}
	}
	// A third independently authenticated connection must share the daemon
	// user's budget. File-data RPCs are intentionally not frontend proxy methods.
	third := h.directFileClient([]string{"file.watch", "file.read"})
	call, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	overflow, err := third.WatchFiles(call, &pb.WatchFilesRequest{ProjectId: project.Id})
	if err == nil {
		_, err = overflow.Receive()
		_ = overflow.Close()
	}
	cancel()
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("33rd watch across connections/credentials: %v", err)
	}
	info, err := third.StatFile(h.ctx, &pb.StatFileRequest{ProjectId: project.Id, Path: "file.txt"})
	if err != nil || info.GetSize() != int64(len(data)) {
		t.Fatalf("watch exhaustion blocked metadata: %v %v", info, err)
	}
	denied := h.directFileClient([]string{"system.info"})
	call, cancel = context.WithTimeout(h.ctx, 5*time.Second)
	stream, err := denied.WatchFiles(call, &pb.WatchFilesRequest{ProjectId: project.Id})
	if err == nil {
		_, err = stream.Receive()
		_ = stream.Close()
	}
	cancel()
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("unauthorized watch checked capacity before permissions: %v", err)
	}
	cancels[0]()
	_ = opened[0].Close()
	h.await("released watch capacity", func(ctx context.Context) error {
		replacement, err := third.WatchFiles(ctx, &pb.WatchFilesRequest{ProjectId: project.Id})
		if err != nil {
			return err
		}
		defer replacement.Close()
		event, err := replacement.Receive()
		if err != nil {
			return err
		}
		if event.GetType() != pb.FileEvent_RESCAN_REQUIRED {
			return fmt.Errorf("replacement was not ready: %v", event)
		}
		return nil
	})
}

func (h *harness) directFileClient(permissions []string) rpc.ShikiDaemonClient {
	h.t.Helper()
	token, _, err := h.daemon.Service.Auth.Create(h.daemonConfig.RunUser, permissions, time.Hour)
	if err != nil {
		h.t.Fatal(err)
	}
	ca, err := os.ReadFile(h.daemonConfig.Server.DirectTLS.CertFile)
	if err != nil {
		h.t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		h.t.Fatal("invalid fixture CA")
	}
	endpoint, err := connection.NewTLSClient("https://"+h.daemonConfig.Server.DirectTLS.Address, connection.TLSOptions{ServerName: "example.com", RootCAs: roots})
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(endpoint.Close)
	credential, err := connection.BearerToken(token)
	if err != nil {
		h.t.Fatal(err)
	}
	return rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, credential))
}
