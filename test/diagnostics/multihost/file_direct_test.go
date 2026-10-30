package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type directFileProbe struct {
	rpc.UnimplementedShikiDaemonHandler
	agreement *pb.ProtocolAgreement
	files     atomic.Int64
}

func (p *directFileProbe) NegotiateProtocol(_ context.Context, offer *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	if offer.Major != 1 || len(offer.RequiredCapabilities) != 1 || offer.RequiredCapabilities[0] != sdk.FileCapability {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "file capability required")
	}
	return p.agreement, nil
}
func (p *directFileProbe) StatFile(context.Context, *pb.StatFileRequest) (*pb.FileInfo, error) {
	p.files.Add(1)
	return &pb.FileInfo{Path: "file.txt", Revision: "revision"}, nil
}
func (p *directFileProbe) WriteFile(_ context.Context, stream rpc.ShikiDaemonWriteFileServerStream) (*pb.FileInfo, error) {
	first, err := stream.Receive()
	if err != nil {
		return nil, err
	}
	if first.GetHeader().GetBaseRevision() != "revision" {
		return nil, connect.NewError(connect.CodeAborted, "wrong revision")
	}
	var content []byte
	for {
		frame, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		content = append(content, frame.GetChunk()...)
	}
	if string(content) != "payload" {
		return nil, connect.NewError(connect.CodeDataLoss, "wrong payload")
	}
	p.files.Add(1)
	return &pb.FileInfo{Path: "file.txt", Revision: "next", Size: int64(len(content))}, nil
}

func TestDirectFilesRequiresAuthenticatedPinnedTLSHTTP2BeforeFileOperations(t *testing.T) {
	for _, scenario := range []string{"ok", "daemon-mismatch", "missing-capability", "protocol-mismatch", "unauthenticated", "wrong-tls-name", "http1", "nonprivate-token", "symlink-token", "pin-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			h := &helper{root: t.TempDir()}
			if err := os.Chmod(h.root, 0700); err != nil {
				t.Fatal(err)
			}
			probe := &directFileProbe{agreement: &pb.ProtocolAgreement{Major: 1, DaemonId: "daemon_expected", Capabilities: []string{sdk.FileCapability}}}
			switch scenario {
			case "daemon-mismatch":
				probe.agreement.DaemonId = "daemon_other"
			case "missing-capability":
				probe.agreement.Capabilities = nil
			case "protocol-mismatch":
				probe.agreement.Minor = 1
			}
			server := connect.NewServer()
			rpc.RegisterShikiDaemonHandler(server, probe)
			handler := connection.NewRPCHandler(server)
			token := "shiki_owned_test_credential"
			var requests atomic.Int64
			ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.ProtoMajor != 2 || r.Header.Get("Authorization") != "Bearer "+token {
					http.Error(w, "denied", http.StatusUnauthorized)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			ts.EnableHTTP2 = scenario != "http1"
			ts.Config.ErrorLog = log.New(io.Discard, "", 0)
			ts.StartTLS()
			defer ts.Close()
			caPath := filepath.Join(h.root, "ca.pem")
			if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			credential := token
			if scenario == "unauthenticated" {
				credential = "shiki_wrong_test_credential"
			}
			tokenPath := filepath.Join(h.root, "device.token")
			if err := os.WriteFile(tokenPath, []byte(credential+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "nonprivate-token":
				if err := os.Chmod(tokenPath, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink-token":
				if err := os.Remove(tokenPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(caPath, tokenPath); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &apb.ServerConfig{Id: "srv_test", DaemonId: "daemon_expected", DirectTlsAddress: strings.TrimPrefix(ts.URL, "https://"), TlsServerName: "example.com", TlsCaFile: caPath}
			if scenario == "wrong-tls-name" {
				cfg.TlsServerName = "wrong.example"
			}
			if scenario == "pin-mismatch" {
				cfg.DaemonId = "daemon_other"
			}
			client, close, err := h.openDirectFiles(t.Context(), cfg, "daemon_expected")
			if scenario != "ok" {
				if err == nil {
					close()
					t.Fatal("accepted invalid direct file channel")
				}
				if probe.files.Load() != 0 {
					t.Fatal("file operation occurred before admission")
				}
				if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), credential) {
					t.Fatal("credential leaked in error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer close()
			info, err := client.StatFile(t.Context(), &pb.StatFileRequest{ProjectId: "proj_test", Path: "file.txt"})
			if err != nil || info.Revision != "revision" {
				t.Fatalf("direct StatFile: %v %v", info, err)
			}
			stream, err := client.WriteFile(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err = stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{ProjectId: "proj_test", Path: "file.txt", BaseRevision: info.Revision}}}); err != nil {
				t.Fatal(err)
			}
			if err = stream.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: []byte("payload")}}); err != nil {
				t.Fatal(err)
			}
			info, err = stream.CloseAndReceive()
			if err != nil || info.Revision != "next" {
				t.Fatalf("direct upload: %v %v", info, err)
			}
			before := requests.Load()
			if _, err = client.GetSystemInfo(t.Context(), &pb.Empty{}); connect.CodeOf(err) != connect.CodePermissionDenied {
				t.Fatalf("file client allowed non-file RPC: %v", err)
			}
			if requests.Load() != before || probe.files.Load() != 2 {
				t.Fatal("method restriction was not local or file requests did not execute")
			}
		})
	}
}

func TestRawFilesRequireExplicitDiagnosticRoute(t *testing.T) {
	h := &helper{}
	for _, method := range []string{"StatFile", "DeleteFile"} {
		request := request{Args: json.RawMessage(`{"service":"remote","method":"` + method + `"}`)}
		if _, err := h.rpc(t.Context(), request); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("allowed raw file through frontend route: %v", err)
		}
	}
	if _, err := h.fileStream(t.Context(), request{Op: "upload", Args: json.RawMessage(`{"content":"x"}`)}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("upload did not require explicit direct route: %v", err)
	}
}
