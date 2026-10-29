//go:build shiki_load_capacity

package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type admissionResult struct {
	LocalLimit, ServerLimit, ConfirmedHeld, TargetAccepted     int
	OverflowRejected                                           bool
	OverflowError                                              string
	OverflowMilliseconds                                       float64
	SupplementaryServerRPC                                     string
	OneReleasedSlotRecovered, AllHashesHalfClose, CountersZero bool
	Terminal                                                   terminalSample
	FrontendDenied, Completed                                  bool
	FrontendProbe, ServerProbe                                 *rpcProbeEvidence
	Checkpoints                                                []admissionCheckpoint
}

func (e *environment) forwardCount(ctx context.Context) uint64 {
	list := take(e.client.Agent().ListForwards(ctx, &pb.Empty{}))
	for _, f := range list.Forwards {
		if f.Spec.Id == e.forward.Spec.Id {
			return f.Connections
		}
	}
	panic("forward disappeared")
}
func (e *environment) admission() {
	for phase, limits := range [][2]int{{128, 256}, {256, 128}} {
		fmt.Printf("admission phase %d local=%d server=%d\n", phase, limits[0], limits[1])
		e.setLimits(limits[0], limits[1])
		current := e.identities()
		for i := 0; i < 5; i++ {
			ensure(current[i] == e.report.Tracked[i], "host/CLI/tmux changed during explicit limit config")
		}
		e.remember(current)
		e.admissionPhase(phase, limits)
	}
}
func (e *environment) admissionPhase(phase int, limits [2]int) {
	ctx, cancel := context.WithTimeout(e.ctx, 120*time.Second)
	defer cancel()
	result := admissionResult{LocalLimit: limits[0], ServerLimit: limits[1]}
	defer func() { save(filepath.Join(e.output, fmt.Sprintf("admission-%d.json", phase)), result) }()
	entries := e.target.prepare(1000+phase, 128, 32<<10)
	address := fmt.Sprintf("127.0.0.1:%d", e.forward.BoundPort)
	_, before, _ := e.target.counts()
	var exchanges []*exchange
	defer func() {
		cancel()
		for _, x := range exchanges {
			_ = x.conn.Close()
		}
		for _, x := range exchanges {
			select {
			case <-x.finished:
			case <-time.After(5 * time.Second):
				panic("held exchange cleanup failed")
			}
		}
	}()
	for _, entry := range entries {
		exchanges = append(exchanges, connectExchange(ctx, address, entry, true))
	}
	waitReady(ctx, entries, e.target)
	for _, x := range exchanges {
		waitPayload(ctx, x)
	}
	until(ctx, func() bool { return e.forwardCount(ctx) == 128 })
	active, accepted, _ := e.target.counts()
	ensure(active == 128 && accepted-before == 128, "default admission fill not observed")
	e.report.Resources = append(e.report.Resources, e.resources("admission-128-held", 100+phase, e.identities()))
	result.ConfirmedHeld = 128
	result.TargetAccepted = accepted - before
	e.admissionCheck(ctx, phase, &result, "filled", exchanges, 128, 0, before+128)
	result.Terminal = e.terminal.sample(ctx, 100+phase)
	started := time.Now()
	overflow := take((&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", address))
	need(overflow.SetDeadline(time.Now().Add(5 * time.Second)))
	_, writeErr := io.WriteString(overflow, fmt.Sprintf("%-64s", "capacity-overflow-must-not-reach-target"))
	one := make([]byte, 1)
	n, readErr := overflow.Read(one)
	_ = overflow.Close()
	ensure(n == 0 && readErr != nil, "overflow stream unexpectedly produced bytes")
	var netErr net.Error
	ensure(!errors.As(readErr, &netErr) || !netErr.Timeout(), "overflow only timed out instead of rejection")
	result.OverflowError = fmt.Sprintf("write=%v read=%v", writeErr, readErr)
	result.OverflowMilliseconds = float64(time.Since(started)) / float64(time.Millisecond)
	result.OverflowRejected = true
	_, after, _ := e.target.counts()
	ensure(after == accepted, "overflow reached target")
	e.admissionCheck(ctx, phase, &result, "after-overflow", exchanges, 128, 0, before+128)
	if limits[1] == 128 {
		// docs13 forbids frontend ForwardPort. Prove that boundary rather than
		// mistaking its PermissionDenied for a server admission response.
		result.FrontendProbe = e.portRPCProbe(ctx, e.client.Remote(e.mapping.ServerId), "frontend-forbidden", phase)
		ensure(result.FrontendProbe.Receive.Code == "permission_denied" && result.FrontendProbe.Receive.Message == "PROXY_METHOD_FORBIDDEN" && result.FrontendProbe.Receive.Remote, "frontend forbidden ForwardPort boundary changed")
		result.FrontendDenied = true
		e.admissionCheck(ctx, phase, &result, "after-frontend-denial", exchanges, 128, 0, before+128)
		direct, closeDirect := e.directDaemon(ctx)
		defer closeDirect()
		result.ServerProbe = e.portRPCProbe(ctx, direct, "authenticated-daemon", phase)
		ensure(result.ServerProbe.Receive.Code == "resource_exhausted" && result.ServerProbe.Receive.Message == "resource limit exceeded" && result.ServerProbe.Receive.Remote, "server default limit did not return remote ResourceExhausted with the safe transport message")
		result.SupplementaryServerRPC = result.ServerProbe.Receive.Code + "/" + result.ServerProbe.Receive.Message
		_, afterProbes, _ := e.target.counts()
		ensure(afterProbes == accepted, "supplemental RPC reached target")
		e.admissionCheck(ctx, phase, &result, "after-daemon-denial", exchanges, 128, 0, before+128)
	}

	// Release exactly one actual connection, then replace it while 127 stay held.
	exchanges[0].release()
	joinExchanges(ctx, exchanges[:1])
	until(ctx, func() bool {
		active, _, _ := e.target.counts()
		select {
		case <-exchanges[0].finished:
		default:
			return false
		}
		return e.forwardCount(ctx) == 127 && active == 127
	})
	e.admissionCheck(ctx, phase, &result, "one-released", exchanges, 127, 1, before+128)
	replacement := &expectation{Header: fmt.Sprintf("%-64s", fmt.Sprintf("capacity-replacement-%d", phase)), Size: 32 << 10, Chunk: make([]byte, 32<<10), Ready: make(chan struct{}), TargetDone: make(chan error, 1), PayloadReady: make(chan struct{})}
	replacement.Hash = sha256.Sum256(replacement.Chunk)
	e.target.mu.Lock()
	e.target.expected[replacement.Header] = replacement
	e.target.mu.Unlock()
	recovered := connectExchange(ctx, address, replacement, true)
	exchanges = append(exchanges, recovered)
	waitReady(ctx, []*expectation{replacement}, e.target)
	waitPayload(ctx, recovered)
	until(ctx, func() bool { return e.forwardCount(ctx) == 128 })
	e.admissionCheck(ctx, phase, &result, "replacement-held", exchanges, 128, 1, before+129)
	result.OneReleasedSlotRecovered = true
	for _, x := range exchanges[1:] {
		x.release()
	}
	joinExchanges(ctx, exchanges[1:])
	for _, x := range exchanges {
		select {
		case <-x.finished:
		case <-ctx.Done():
			panic(ctx.Err())
		}
	}
	e.settled(ctx)
	e.report.Resources = append(e.report.Resources, e.resources("admission-settled", 100+phase, e.identities()))
	e.admissionCheck(ctx, phase, &result, "settled", exchanges, 0, 129, before+129)
	result.AllHashesHalfClose = true
	result.CountersZero = true
	result.Completed = true
	e.report.Admission = append(e.report.Admission, result)
	save(e.output+"/progress.json", e.report)
}
func waitPayload(ctx context.Context, x *exchange) {
	select {
	case <-x.entry.PayloadReady:
	case <-ctx.Done():
		panic(ctx.Err())
	}
	select {
	case <-x.PayloadEcho:
	case <-ctx.Done():
		panic(ctx.Err())
	}
}

// These diagnostic clients use the actual authenticated HTTP/2 transport. The
// direct client is supplemental only; all holders/overflow/replacement streams
// remain on the normal local worker -> CoreAPI -> daemon -> server worker path.
type rpcErrorEvidence struct {
	Code, Message string
	Remote        bool
}
type rpcProbeEvidence struct {
	Route                string
	Send, Receive, Close rpcErrorEvidence
	ReceivedType         string
}

func rpcErrorRecord(err error, secret string) rpcErrorEvidence {
	if err == nil {
		return rpcErrorEvidence{Code: "ok"}
	}
	value := rpcErrorEvidence{Code: connect.CodeOf(err).String(), Message: err.Error()}
	var coded *connect.Error
	if errors.As(err, &coded) {
		value.Message = coded.Message()
		value.Remote = coded.IsRemote()
	}
	if secret != "" {
		value.Message = strings.ReplaceAll(value.Message, secret, "<redacted>")
	}
	if len(value.Message) > 512 {
		value.Message = value.Message[:512] + "<truncated>"
	}
	return value
}
func (e *environment) portRPCProbe(parent context.Context, client rpc.ShikiDaemonClient, route string, phase int) *rpcProbeEvidence {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	result := &rpcProbeEvidence{Route: route}
	secret := strings.TrimSpace(string(take(os.ReadFile(filepath.Join(e.root, "device.token")))))
	path := filepath.Join(e.output, fmt.Sprintf("admission-%d-rpc-%s.json", phase, route))
	stream, err := client.ForwardPort(ctx)
	if err != nil {
		result.Receive = rpcErrorRecord(err, secret)
		save(path, result)
		return result
	}
	result.Send = rpcErrorRecord(stream.Send(&pb.PortData{Type: pb.PortData_CONNECT, ConnectionId: "capacity-server-limit-probe", RemoteAddress: "127.0.0.1", Port: int32(e.target.listener.Addr().(*net.TCPAddr).Port)}), secret)
	response, err := stream.Receive()
	result.Receive = rpcErrorRecord(err, secret)
	if response != nil {
		result.ReceivedType = response.Type.String()
	}
	result.Close = rpcErrorRecord(stream.Close(), secret)
	save(path, result)
	return result
}
func (e *environment) directDaemon(ctx context.Context) (rpc.ShikiDaemonClient, func()) {
	roots := x509.NewCertPool()
	ensure(roots.AppendCertsFromPEM(take(os.ReadFile(filepath.Join(e.root, "tls.pem")))), "private fixture CA invalid")
	endpoint := take(connection.NewTLSClient("https://"+e.directAddress, connection.TLSOptions{ServerName: "localhost", RootCAs: roots}))
	keep := false
	defer func() {
		if !keep {
			endpoint.Close()
		}
	}()
	token := strings.TrimSpace(string(take(os.ReadFile(filepath.Join(e.root, "device.token")))))
	client := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, take(connection.BearerToken(token))))
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	agreement := take(client.NegotiateProtocol(call, &pb.ProtocolOffer{Major: 1, Capabilities: []string{"port.tcp-half-close.v1"}}))
	ensure(agreement.Major == 1 && agreement.Minor == 0, "diagnostic direct protocol incompatible")
	supported := false
	for _, capability := range agreement.Capabilities {
		if capability == "port.tcp-half-close.v1" {
			supported = true
		}
	}
	ensure(supported, "diagnostic direct port capability missing")
	keep = true
	return client, endpoint.Close
}

// Preserve the observation before checking it. An overflow may be rejected
// while incorrectly cancelling other connections; a TCP reset alone is not
// sufficient evidence of isolated admission.
type admissionCheckpoint struct {
	Name                                             string
	At                                               int64
	ExpectedHeld, ExpectedFinished, ExpectedAccepted int
	TargetActive, TargetAccepted, FinishedExchanges  int
	AgentConnections                                 uint64
	BoundPort                                        uint32
	State, Reason, ObservationError                  string
}

func (e *environment) admissionCheck(ctx context.Context, phase int, result *admissionResult, name string, exchanges []*exchange, held, finished, accepted int) {
	value := admissionCheckpoint{Name: name, At: time.Now().UnixNano(), ExpectedHeld: held, ExpectedFinished: finished, ExpectedAccepted: accepted}
	value.TargetActive, value.TargetAccepted, _ = e.target.counts()
	for _, x := range exchanges {
		select {
		case <-x.finished:
			value.FinishedExchanges++
		default:
		}
	}
	states, err := e.client.Agent().ListForwards(ctx, &pb.Empty{})
	if err != nil {
		value.ObservationError = fmt.Sprint(err)
	} else {
		found := false
		for _, state := range states.Forwards {
			if state.Spec.Id == e.forward.Spec.Id {
				found = true
				value.AgentConnections = state.Connections
				value.BoundPort = state.BoundPort
				value.State = state.State
				value.Reason = state.ReasonCode
			}
		}
		if !found {
			value.ObservationError = "forward disappeared"
		}
	}
	result.Checkpoints = append(result.Checkpoints, value)
	save(filepath.Join(e.output, fmt.Sprintf("admission-%d.json", phase)), result)
	ensure(value.ObservationError == "" && value.TargetActive == held && value.AgentConnections == uint64(held) && value.FinishedExchanges == finished && value.TargetAccepted == accepted && value.State == "listening" && value.BoundPort == e.forward.BoundPort, "admission checkpoint "+name+" violated held connections/listener/target isolation")
}
