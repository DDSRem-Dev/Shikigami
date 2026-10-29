package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type Scanner interface {
	Scan(context.Context) ([]*pb.OpenPort, error)
}
type SystemScanner struct{}
type boundedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedOutput) Len() int      { return b.buffer.Len() }
func (b *boundedOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("port scan output limit exceeded")
	}
	return b.buffer.Write(p)
}
func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	out := &boundedOutput{limit: 1 << 20}
	stderr := &boundedOutput{limit: 16 << 10}
	cmd.Stdout = out
	cmd.Stderr = stderr
	err := cmd.Run()
	if stderr.Len() != 0 {
		return out.Bytes(), errors.New("port scanner reported incomplete results")
	}
	if err != nil {
		return out.Bytes(), fmt.Errorf("port scanner %s failed: %w", name, err)
	}
	return out.Bytes(), nil
}
func (SystemScanner) Scan(ctx context.Context) ([]*pb.OpenPort, error) {
	var ports []*pb.OpenPort
	switch runtime.GOOS {
	case "linux":
		for _, family := range []string{"-4", "-6"} {
			out, err := command(ctx, "ss", "-H", "-ltnp", family)
			if err != nil {
				return nil, err
			}
			parsed, err := parseSS(out, family == "-6")
			if err != nil {
				return nil, err
			}
			ports = append(ports, parsed...)
		}
	case "darwin":
		out, err := command(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcnt0")
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(out) != 0 {
				return nil, err
			}
		}
		ports, err = parseLsof(out)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("port scanning unsupported on this platform")
	}
	unique := map[string]*pb.OpenPort{}
	for _, p := range ports {
		key := endpointKey(p)
		if old := unique[key]; old != nil {
			if old.Pid != p.Pid {
				old.Pid = 0
				old.ProcessName = ""
				old.SessionId = ""
			}
		} else {
			unique[key] = p
		}
	}
	ports = nil
	for _, p := range unique {
		ports = append(ports, p)
	}
	sort.Slice(ports, func(i, j int) bool { return endpointKey(ports[i]) < endpointKey(ports[j]) })
	return ports, nil
}
func endpointKey(p *pb.OpenPort) string {
	return net.JoinHostPort(p.BindAddress, strconv.Itoa(int(p.Port)))
}
func endpoint(value string, ipv6 bool) (string, int32, error) {
	idx := strings.LastIndex(value, ":")
	if idx < 1 {
		return "", 0, errors.New("invalid scanner endpoint")
	}
	host := strings.Trim(value[:idx], "[]")
	if host == "*" {
		host = "0.0.0.0"
		if ipv6 {
			host = "::"
		}
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.ParseUint(value[idx+1:], 10, 16)
	if err != nil || port == 0 {
		return "", 0, errors.New("invalid scanner port")
	}
	return addr.String(), int32(port), nil
}

var processPattern = regexp.MustCompile(`\("([^"\r\n]+)",pid=([0-9]+),`)

func parseSS(data []byte, ipv6 bool) ([]*pb.OpenPort, error) {
	var result []*pb.OpenPort
	s := bufio.NewScanner(bytes.NewReader(data))
	s.Buffer(make([]byte, 4096), 1<<20)
	for s.Scan() {
		line := s.Text()
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "LISTEN" {
			return nil, errors.New("unrecognized ss record")
		}
		address, port, err := endpoint(fields[3], ipv6)
		if err != nil {
			return nil, err
		}
		p := &pb.OpenPort{BindAddress: address, Port: port, Protocol: "tcp"}
		matches := processPattern.FindAllStringSubmatch(line, -1)
		if len(matches) == 1 {
			pid, err := strconv.ParseInt(matches[0][2], 10, 32)
			if err != nil {
				return nil, err
			}
			p.Pid = int32(pid)
			p.ProcessName = matches[0][1]
		}
		result = append(result, p)
	}
	return result, s.Err()
}
func parseLsof(data []byte) ([]*pb.OpenPort, error) {
	var result []*pb.OpenPort
	var pid int32
	var process string
	var ipv6 bool
	var familyKnown bool
	for _, raw := range bytes.Split(data, []byte{0}) {
		field := strings.TrimLeft(string(raw), "\n")
		if field == "" {
			continue
		}
		switch field[0] {
		case 'p':
			v, err := strconv.ParseInt(field[1:], 10, 32)
			if err != nil || v <= 0 {
				return nil, errors.New("invalid lsof PID")
			}
			pid = int32(v)
			process = ""
		case 'c':
			process = field[1:]
		case 'f':
			familyKnown = false
		case 't':
			if field[1:] != "IPv4" && field[1:] != "IPv6" {
				return nil, errors.New("unexpected lsof socket family")
			}
			ipv6 = field[1:] == "IPv6"
			familyKnown = true
		case 'n':
			if !familyKnown {
				return nil, errors.New("missing lsof socket family")
			}
			if pid == 0 {
				return nil, errors.New("lsof endpoint without process")
			}
			address, port, err := endpoint(field[1:], ipv6)
			if err != nil {
				return nil, err
			}
			result = append(result, &pb.OpenPort{BindAddress: address, Port: port, Protocol: "tcp", Pid: pid, ProcessName: process})
		default:
			return nil, errors.New("unexpected lsof field")
		}
	}
	return result, nil
}
