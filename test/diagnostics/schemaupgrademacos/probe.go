//go:build darwin && schemamacos

// This opt-in fixture binary probes a fixed private test daemon. It is never
// shipped, and neither accepts nor prints a credential in argv/environment.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/shirou/gopsutil/v4/process"
)

var fixtureOrigin string

type responseEvidence struct {
	Status      int    `json:"http_status"`
	HTTP2       bool   `json:"http2"`
	TLSVerified bool   `json:"tls_verified"`
	Certificate string `json:"peer_certificate_sha256"`
}

// Record only public transport metadata. Request/response bodies and headers
// are deliberately absent, including on failures.
type observedTransport struct {
	next http.RoundTripper
	mu   sync.Mutex
	seen []responseEvidence
}

func (t *observedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if response != nil {
		entry := responseEvidence{Status: response.StatusCode, HTTP2: response.ProtoMajor == 2}
		if response.TLS != nil {
			entry.TLSVerified = len(response.TLS.VerifiedChains) > 0 && response.TLS.NegotiatedProtocol == "h2"
			if len(response.TLS.PeerCertificates) > 0 {
				digest := sha256.Sum256(response.TLS.PeerCertificates[0].Raw)
				entry.Certificate = hex.EncodeToString(digest[:])
			}
		}
		t.mu.Lock()
		t.seen = append(t.seen, entry)
		t.mu.Unlock()
	}
	return response, err
}

func (t *observedTransport) drain() []responseEvidence {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := t.seen
	t.seen = nil
	return result
}

type callEvidence struct {
	Method   string             `json:"method"`
	Code     string             `json:"code"`
	Remote   bool               `json:"remote_error"`
	Response []responseEvidence `json:"responses"`
	Matched  bool               `json:"matched_expected_outcome"`
}

type credentialEvidence struct {
	Role     string         `json:"role"`
	DaemonID string         `json:"daemon_id,omitempty"`
	Calls    []callEvidence `json:"calls"`
	Passed   bool           `json:"passed"`
}

type probeEvidence struct {
	Origin            string               `json:"origin"`
	ProjectID         string               `json:"project_id"`
	UID               int                  `json:"uid"`
	ExpectedRevoked   bool                 `json:"expected_revoked"`
	Credentials       []credentialEvidence `json:"credentials"`
	Passed            bool                 `json:"passed"`
	PreparationReason string               `json:"preparation_reason,omitempty"`
}

func privateFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("PRIVATE_FILE_UNAVAILABLE")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() <= 0 || info.Size() > maximum {
		return nil, errors.New("INVALID_PRIVATE_FILE")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Geteuid() {
		return nil, errors.New("PRIVATE_FILE_OWNER_MISMATCH")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, errors.New("PRIVATE_FILE_READ_FAILED")
	}
	return data, nil
}

func probe(role, tokenFile, project string, roots *x509.CertPool, revoked bool) credentialEvidence {
	result := credentialEvidence{Role: role, Passed: true}
	data, err := privateFile(tokenFile, 16384)
	if err != nil {
		result.Passed = false
		return result
	}
	defer clear(data)
	authorize, err := connection.BearerToken(strings.TrimSpace(string(data)))
	if err != nil {
		result.Passed = false
		return result
	}
	endpoint, err := connection.NewTLSClient(fixtureOrigin, connection.TLSOptions{ServerName: "localhost", RootCAs: roots})
	if err != nil {
		result.Passed = false
		return result
	}
	defer endpoint.Close()
	observed := &observedTransport{next: endpoint.HTTPClient.Transport}
	endpoint.HTTPClient.Transport = observed
	client := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, authorize))
	call := func(method string, operation func(context.Context) (bool, error)) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		valid, err := operation(ctx)
		receipt := callEvidence{Method: method, Code: "ok", Response: observed.drain()}
		if err != nil {
			receipt.Code = connect.CodeOf(err).String()
			var remote *connect.Error
			receipt.Remote = errors.As(err, &remote) && remote.IsRemote()
		}
		receipt.Matched = err == nil && valid
		if revoked {
			receipt.Matched = connect.CodeOf(err) == connect.CodeUnauthenticated && receipt.Remote
		}
		if len(receipt.Response) != 1 {
			receipt.Matched = false
		}
		for _, response := range receipt.Response {
			receipt.Matched = receipt.Matched && response.HTTP2 && response.TLSVerified && response.Certificate != ""
		}
		result.Passed = result.Passed && receipt.Matched
		result.Calls = append(result.Calls, receipt)
	}
	call("NegotiateProtocol", func(ctx context.Context) (bool, error) {
		agreement, err := client.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1})
		return agreement != nil && agreement.Major == 1 && agreement.Minor == 0, err
	})
	// Even when negotiation is rejected, issue both protected RPCs so the
	// evidence proves their actual remote rejection rather than client gating.
	call("GetSystemInfo", func(ctx context.Context) (bool, error) {
		info, err := client.GetSystemInfo(ctx, &pb.Empty{})
		if info != nil {
			result.DaemonID = info.DaemonId
		}
		return info != nil && strings.HasPrefix(info.DaemonId, "daemon_"), err
	})
	if project != "" {
		call("GetProject", func(ctx context.Context) (bool, error) {
			info, err := client.GetProject(ctx, &pb.GetProjectRequest{Id: project})
			return info != nil && info.Id == project, err
		})
	}
	return result
}

func main() {
	root := flag.String("root", "", "Private native fixture root")
	pid := flag.Int("inspect", 0, "Read a same-user process identity")
	project := flag.String("project", "", "Existing fixture project ID")
	ready := flag.Bool("ready", false, "Probe only the original credential before fixture seeding")
	revoked := flag.Bool("expect-revoked", false, "Require remote Unauthenticated for the candidate")
	flag.Parse()
	if os.Geteuid() == 0 || filepath.Dir(*root) != "/private/tmp" || !strings.HasPrefix(filepath.Base(*root), "shiki-schema-macos.") || flag.NArg() != 0 {
		os.Exit(2)
	}
	if *pid != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exists, err := process.PidExistsWithContext(ctx, int32(*pid))
		if err != nil {
			os.Exit(1)
		}
		if !exists {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"state": "absent"})
			return
		}
		p, err := process.NewProcessWithContext(ctx, int32(*pid))
		if err != nil {
			os.Exit(1)
		}
		created, err := p.CreateTimeWithContext(ctx)
		if err != nil {
			os.Exit(1)
		}
		executable, err := p.ExeWithContext(ctx)
		if err != nil {
			os.Exit(1)
		}
		uids, err := p.UidsWithContext(ctx)
		if err != nil || len(uids) == 0 {
			os.Exit(1)
		}
		for _, uid := range uids {
			if int(uid) != os.Getuid() {
				os.Exit(1)
			}
		}
		fresh, err := process.NewProcessWithContext(ctx, int32(*pid))
		if err != nil {
			os.Exit(1)
		}
		again, err := fresh.CreateTimeWithContext(ctx)
		if err != nil || again != created {
			os.Exit(1)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"state": "present", "pid": *pid, "created_ms": created, "executable": executable, "uids": uids})
		return
	}
	runtime := filepath.Join(*root, "runtime")
	address, err := privateFile(filepath.Join(runtime, "address"), 256)
	if err != nil {
		os.Exit(1)
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(string(address)))
	number, numberErr := strconv.Atoi(port)
	if err != nil || numberErr != nil || host != "127.0.0.1" || number < 1 || number > 65535 {
		os.Exit(1)
	}
	fixtureOrigin = "https://" + net.JoinHostPort(host, port)
	result := probeEvidence{Origin: fixtureOrigin, ProjectID: *project, UID: os.Geteuid(), ExpectedRevoked: *revoked}
	if (!*ready && !pb.ValidResourceID(*project, "proj")) || (*ready && (*project != "" || *revoked)) {
		result.PreparationReason = "INVALID_FIXTURE_CONTEXT"
	} else if certificate, err := privateFile(filepath.Join(runtime, "tls/cert.pem"), 1<<20); err != nil {
		result.PreparationReason = "FIXTURE_CA_UNAVAILABLE"
	} else {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(certificate) {
			result.PreparationReason = "FIXTURE_CA_INVALID"
		} else {
			valid := probe("valid", filepath.Join(runtime, "original-device.token"), *project, roots, false)
			result.Credentials = []credentialEvidence{valid}
			result.Passed = valid.Passed
			if !*ready {
				candidate := probe("candidate", filepath.Join(runtime, "must-stay-revoked.token"), *project, roots, *revoked)
				result.Credentials = append(result.Credentials, candidate)
				result.Passed = result.Passed && candidate.Passed && (*revoked || valid.DaemonID == candidate.DaemonID)
			}
		}
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil || !result.Passed {
		os.Exit(1)
	}
}
