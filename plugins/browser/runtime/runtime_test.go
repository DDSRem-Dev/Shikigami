package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/diagnostics"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func TestUnitIdentityRefusesUnrelatedOrChangedLaunch(t *testing.T) {
	spec := Spec{InstanceID: "instance", ConfigPath: "/private/runtime/instance.json"}
	valid := map[string]string{"Description": "ShikigamiBrowser:instance", "Transient": "yes", "ExecStart": "{ path=" + Binary + " ; argv[]=" + Binary + " --config " + spec.ConfigPath + " ; ignore_errors=no ; }"}
	if err := verifyUnit(valid, spec); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"Description", "Transient", "ExecStart"} {
		original := valid[key]
		valid[key] = "unrelated"
		if err := verifyUnit(valid, spec); err == nil {
			t.Fatalf("accepted changed %s", key)
		}
		valid[key] = original
	}
}
func TestDiagnosticEnvelopeRejectsCredentialsRedirectsAndUnapprovedReads(t *testing.T) {
	for _, method := range []string{"GET", "POST", "OPTIONS"} {
		if !diagnosticEnvelopeAllowed(true, method, "", nil) {
			t.Fatalf("explicit authorized native %s rejected", method)
		}
		for _, header := range []string{"Cookie", "cookie", "Authorization", "Proxy-Authorization", "Referer"} {
			if diagnosticEnvelopeAllowed(true, method, "", map[string]any{header: "secret"}) {
				t.Fatalf("%s accepted credential/referrer %s", method, header)
			}
		}
		if diagnosticEnvelopeAllowed(false, method, "", nil) || diagnosticEnvelopeAllowed(true, method, "redirect-id", nil) {
			t.Fatalf("accepted unapproved/redirected %s", method)
		}
	}
	if diagnosticEnvelopeAllowed(true, "DELETE", "", nil) {
		t.Fatal("accepted unauthorized mutation method")
	}
	request := &epb.DiagnosticRequest{ProfileId: "one", AllowExternal: true, Providers: []*epb.ProbeProvider{{Id: "explicit", Kind: "ip-echo", Url: "https://approved.invalid/ip"}}}
	for _, url := range []string{"http://approved.invalid/ip", "https://approved.invalid/other", "https://user:password@approved.invalid/ip", "https://127.0.0.1/ip"} {
		if err := diagnostics.ValidateBrowserProbeURL(context.Background(), request, url); err == nil {
			t.Fatalf("accepted non-allowlisted URL %s", url)
		}
	}
}

// The pipe fixture verifies target scoping and fixed source, not Chrome's native
// behavior. Real Chrome/display/keyring acceptance remains an integration gate.
func diagnosticPipe(t *testing.T, result string) (*cdpClient, func() []string) {
	t.Helper()
	clientSide, chromeSide := net.Pipe()
	client := newCDP(clientSide, clientSide)
	var mu sync.Mutex
	var methods []string
	t.Cleanup(func() { client.Close(); chromeSide.Close() })
	go func() {
		reader := bufio.NewReader(chromeSide)
		for {
			data, err := reader.ReadBytes(0)
			if err != nil {
				return
			}
			var message struct {
				ID     int64                      `json:"id"`
				Method string                     `json:"method"`
				Params map[string]json.RawMessage `json:"params"`
			}
			if json.Unmarshal(data[:len(data)-1], &message) != nil {
				return
			}
			mu.Lock()
			methods = append(methods, message.Method)
			mu.Unlock()
			response := any(map[string]any{})
			switch message.Method {
			case "Target.createTarget":
				response = map[string]any{"targetId": "dedicated"}
			case "Target.attachToTarget":
				response = map[string]any{"sessionId": "private-session"}
			case "Runtime.evaluate":
				var expression string
				json.Unmarshal(message.Params["expression"], &expression)
				if expression == "location.href" {
					response = map[string]any{"result": map[string]any{"value": diagnostics.BrowserDiagnosticURL}}
				} else {
					if !strings.Contains(expression, "shikigami-diagnostics-v1") {
						return
					}
					response = map[string]any{"result": map[string]any{"value": result}}
				}
			}
			encoded, _ := json.Marshal(map[string]any{"id": message.ID, "result": response})
			if _, err := chromeSide.Write(append(encoded, 0)); err != nil {
				return
			}
		}
	}()
	return client, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), methods...) }
}
func contextReport(profile string) string {
	now := time.Now().Unix()
	identity, _ := json.Marshal(map[string]string{"profile_id": profile, "target_url": diagnostics.BrowserDiagnosticURL})
	report := &epb.DiagnosticReport{ObservedAt: now, Observations: []*epb.DiagnosticObservation{{Check: "browser.context", State: "observed", Value: string(identity), Source: "fixed diagnostic JavaScript in managed server Chrome", Vantage: "server-chrome:" + profile, ObservedAt: now, Limitation: "Dedicated fixed target, not the local viewer."}}}
	data, _ := json.Marshal(report)
	return string(data)
}
func TestDiagnosticUsesOnlyCurrentChromeDedicatedTargetAndClosesIt(t *testing.T) {
	client, methods := diagnosticPipe(t, contextReport("one"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	report, err := client.diagnose(ctx, &epb.DiagnosticRequest{ProfileId: "one"})
	if err != nil || len(report.Observations) != 1 {
		t.Fatalf("diagnostic %#v %v", report, err)
	}
	seenClose := false
	for _, method := range methods() {
		if method == "Target.createBrowserContext" || strings.Contains(method, "Proxy") {
			t.Fatalf("changed native profile/network: %s", method)
		}
		if method == "Target.closeTarget" {
			seenClose = true
		}
	}
	if !seenClose {
		t.Fatal("dedicated target leaked")
	}
}
func TestDiagnosticRejectsDifferentProfileAndMalformedResult(t *testing.T) {
	for _, result := range []string{contextReport("other"), `{"observations":[{"state":"pass"}]}`, strings.Repeat("x", (1<<20)+1)} {
		client, _ := diagnosticPipe(t, result)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := client.diagnose(ctx, &epb.DiagnosticRequest{ProfileId: "one"})
		cancel()
		client.Close()
		if err == nil {
			t.Fatal("accepted unbound/unbounded diagnostic result")
		}
	}
}
func TestCDPRejectsOversizedPipeMessage(t *testing.T) {
	left, right := net.Pipe()
	client := newCDP(left, left)
	defer client.Close()
	defer right.Close()
	go func() { _, _ = io.Copy(right, strings.NewReader(strings.Repeat("x", (2<<20)+1)+"\x00")) }()
	select {
	case <-client.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("oversized pipe message did not close")
	}
}
