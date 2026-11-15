package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/diagnostics"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type cdpClient struct {
	input    io.WriteCloser
	output   io.ReadCloser
	write    sync.Mutex
	mu       sync.Mutex
	pending  map[int64]chan cdpMessage
	events   map[string]chan cdpMessage
	sequence atomic.Int64
	closed   chan struct{}
	once     sync.Once
}

func newCDP(input io.WriteCloser, output io.ReadCloser) *cdpClient {
	client := &cdpClient{input: input, output: output, pending: make(map[int64]chan cdpMessage), events: make(map[string]chan cdpMessage), closed: make(chan struct{})}
	go client.receive()
	return client
}
func (client *cdpClient) Close() error {
	client.once.Do(func() { close(client.closed); client.input.Close(); client.output.Close() })
	return nil
}
func (client *cdpClient) receive() {
	defer client.Close()
	reader := bufio.NewReaderSize(client.output, 64<<10)
	for {
		var data []byte
		for {
			part, err := reader.ReadSlice(0)
			if len(data)+len(part) > 2<<20 {
				return
			}
			data = append(data, part...)
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if err != nil {
				return
			}
			break
		}
		var message cdpMessage
		if json.Unmarshal(data[:len(data)-1], &message) != nil {
			return
		}
		client.mu.Lock()
		if message.ID != 0 {
			if result := client.pending[message.ID]; result != nil {
				select {
				case result <- message:
				default:
				}
			}
		} else if queue := client.events[message.SessionID]; queue != nil {
			select {
			case queue <- message:
			default:
				client.mu.Unlock()
				return
			}
		}
		client.mu.Unlock()
	}
}
func (client *cdpClient) call(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	id := client.sequence.Add(1)
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	message, err := json.Marshal(cdpMessage{ID: id, SessionID: session, Method: method, Params: encoded})
	if err != nil {
		return nil, err
	}
	result := make(chan cdpMessage, 1)
	client.mu.Lock()
	client.pending[id] = result
	client.mu.Unlock()
	defer func() { client.mu.Lock(); delete(client.pending, id); client.mu.Unlock() }()
	client.write.Lock()
	_, err = client.input.Write(append(message, 0))
	client.write.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case response := <-result:
		if response.Error != nil {
			return nil, fmt.Errorf("Chrome diagnostic method %s failed (%d)", method, response.Error.Code)
		}
		return response.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-client.closed:
		return nil, errors.New("Chrome private CDP pipe closed")
	}
}

// diagnose does not expose evaluate: the only expression is SDK fixed source.
// It uses the existing Chrome/default profile, never a proxy or new context.
func (client *cdpClient) diagnose(parent context.Context, request *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	script, err := diagnostics.BrowserScript(request)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 75*time.Second)
	defer cancel()
	created, err := client.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank", "background": true})
	if err != nil {
		return nil, err
	}
	var target struct {
		TargetID string `json:"targetId"`
	}
	if json.Unmarshal(created, &target) != nil || target.TargetID == "" {
		return nil, errors.New("Chrome did not create diagnostic target")
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_, _ = client.call(cleanup, "", "Target.closeTarget", map[string]any{"targetId": target.TargetID})
	}()
	attached, err := client.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true})
	if err != nil {
		return nil, err
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(attached, &session) != nil || session.SessionID == "" {
		return nil, errors.New("Chrome did not attach diagnostic target")
	}
	events := make(chan cdpMessage, 128)
	client.mu.Lock()
	client.events[session.SessionID] = events
	client.mu.Unlock()
	defer func() { client.mu.Lock(); delete(client.events, session.SessionID); client.mu.Unlock() }()
	if _, err := client.call(ctx, session.SessionID, "Page.enable", nil); err != nil {
		return nil, err
	}
	if _, err := client.call(ctx, session.SessionID, "Network.enable", map[string]any{"maxTotalBufferSize": 1 << 20, "maxResourceBufferSize": 256 << 10}); err != nil {
		return nil, err
	}
	if _, err := client.call(ctx, session.SessionID, "Fetch.enable", map[string]any{"patterns": []any{map[string]any{"urlPattern": "http://*", "requestStage": "Request"}, map[string]any{"urlPattern": "https://*", "requestStage": "Request"}}}); err != nil {
		return nil, err
	}
	if _, err := client.call(ctx, session.SessionID, "Page.navigate", map[string]any{"url": diagnostics.BrowserDiagnosticURL}); err != nil {
		return nil, err
	}
	// The diagnostic data page has no subresources, but wait for its exact URL
	// before executing the fixed script and never inspect the user's tabs.
	for {
		location, err := client.call(ctx, session.SessionID, "Runtime.evaluate", map[string]any{"expression": "location.href", "returnByValue": true})
		if err != nil {
			return nil, err
		}
		var current struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(location, &current) == nil && current.Result.Value == diagnostics.BrowserDiagnosticURL {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	type evaluation struct {
		raw json.RawMessage
		err error
	}
	completed := make(chan evaluation, 1)
	go func() {
		result, err := client.call(ctx, session.SessionID, "Runtime.evaluate", map[string]any{"expression": script, "awaitPromise": true, "returnByValue": true, "timeout": 65000})
		completed <- evaluation{result, err}
	}()
	observed := make(map[string]bool)
	var remoteObservations []*epb.DiagnosticObservation
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-completed:
			if result.err != nil {
				return nil, result.err
			}
			var value struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
				Exception json.RawMessage `json:"exceptionDetails"`
			}
			if json.Unmarshal(result.raw, &value) != nil || len(value.Exception) != 0 || len(value.Result.Value) > 1<<20 {
				return nil, errors.New("invalid fixed Chrome diagnostic result")
			}
			report, err := diagnostics.DecodeBrowserReport([]byte(value.Result.Value))
			if err != nil {
				return nil, err
			}
			for _, observation := range report.Observations {
				if observation.Vantage != "server-chrome:"+request.ProfileId && !strings.HasSuffix(observation.Vantage, ":server-chrome:"+request.ProfileId) {
					return nil, errors.New("Chrome diagnostic result changed profile scope")
				}
			}
			report.Observations = append(report.Observations, remoteObservations...)
			if request.AllowExternal && len(observed) == 0 {
				report.Observations = append(report.Observations, &epb.DiagnosticObservation{Check: "browser.remote_address", State: "unknown", Source: "Chrome CDP Network.responseReceived", Vantage: "server-chrome:" + request.ProfileId, ObservedAt: time.Now().Unix(), Limitation: "Chrome did not expose an actual remote IP; no public-address success is inferred."})
			}
			report.Warnings = append(report.Warnings, "Native Chrome requests retain the target profile network and Secure DNS. Public DNS preflight and post-connect observations cannot guarantee DNS-rebinding protection before connection; this is not a network sandbox.")
			return report, nil
		case event := <-events:
			switch event.Method {
			case "Fetch.requestPaused":
				var paused struct {
					RequestID           string `json:"requestId"`
					RedirectedRequestID string `json:"redirectedRequestId"`
					Request             struct {
						URL     string         `json:"url"`
						Method  string         `json:"method"`
						Headers map[string]any `json:"headers"`
					} `json:"request"`
				}
				if json.Unmarshal(event.Params, &paused) != nil {
					return nil, errors.New("invalid Chrome network interception")
				}
				allowed := diagnosticEnvelopeAllowed(request.AllowExternal, paused.Request.Method, paused.RedirectedRequestID, paused.Request.Headers)
				if allowed {
					allowed = diagnostics.ValidateBrowserProbeURL(ctx, request, paused.Request.URL) == nil
				}
				method, params := "Fetch.failRequest", map[string]any{"requestId": paused.RequestID, "errorReason": "BlockedByClient"}
				if allowed {
					method, params = "Fetch.continueRequest", map[string]any{"requestId": paused.RequestID}
				}
				if _, err := client.call(ctx, session.SessionID, method, params); err != nil {
					return nil, err
				}
			case "Network.responseReceived":
				var response struct {
					Response struct {
						URL             string `json:"url"`
						RemoteIPAddress string `json:"remoteIPAddress"`
					} `json:"response"`
				}
				if json.Unmarshal(event.Params, &response) != nil {
					return nil, errors.New("invalid Chrome response observation")
				}
				address := response.Response.RemoteIPAddress
				if address == "" || observed[address] {
					continue
				}
				observed[address] = true
				state, limitation := "observed", "Observed only after connection; does not prove pre-connect DNS pinning."
				if !diagnostics.IsPublicAddress(address) {
					state, limitation = "warning", "Actual Chrome connection used a non-public remote address; external endpoint evidence is not a pass."
				}
				remoteObservations = append(remoteObservations, &epb.DiagnosticObservation{Check: "browser.remote_address", State: state, Value: address, Source: "Chrome CDP Network.responseReceived", Vantage: "server-chrome:" + request.ProfileId, ObservedAt: time.Now().Unix(), Limitation: limitation})
			}
		}
	}
}

func diagnosticEnvelopeAllowed(external bool, method, redirect string, headers map[string]any) bool {
	// The fixed JSON DNS-observer POST requires a browser CORS preflight.
	// OPTIONS still passes the same provider URL and credential checks.
	if !external || redirect != "" || (method != "GET" && method != "POST" && method != "OPTIONS") {
		return false
	}
	for name := range headers {
		if strings.EqualFold(name, "Cookie") || strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Proxy-Authorization") || strings.EqualFold(name, "Referer") {
			return false
		}
	}
	return true
}
