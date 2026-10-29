//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

func prepareOwnedPortFixture(t *testing.T, ctx context.Context, dir, bin string) (string, int, string) {
	t.Helper()
	reservation, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	reservation.Close()
	source := filepath.Join(dir, "owned-service.go")
	executable := filepath.Join(bin, "owned-service")
	program := fmt.Sprintf(`package main
import("fmt";"io";"net";"os")
func main(){if len(os.Args)>1&&os.Args[1]=="--version"{fmt.Println("test-owned-service");return};l,e:=net.Listen("tcp4",%q);if e!=nil{panic(e)};defer l.Close();for{c,e:=l.Accept();if e!=nil{return};go func(){defer c.Close();data,e:=io.ReadAll(c);if e==nil{c.Write(append([]byte("owned:"),data...))}}()}}
`, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if e = os.WriteFile(source, []byte(program), 0600); e != nil {
		t.Fatal(e)
	}
	command := exec.CommandContext(ctx, "go", "build", "-race", "-o", executable, source)
	if out, e := command.CombinedOutput(); e != nil {
		t.Fatalf("owned fixture: %v %s", e, out)
	}
	scanner := "ss"
	if runtime.GOOS == "darwin" {
		scanner = "lsof"
	}
	actual, e := exec.LookPath(scanner)
	if e != nil {
		t.Fatal(e)
	}
	failure := filepath.Join(dir, "scanner-failure")
	script := fmt.Sprintf("#!/bin/sh\nif [ -f '%s' ]; then echo scanner-unavailable >&2; exit 1; fi\nexec '%s' \"$@\"\n", failure, actual)
	if e = os.WriteFile(filepath.Join(bin, scanner), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	return executable, port, failure
}
func exerciseAutomaticPorts(t *testing.T, ctx context.Context, dir string, ownedPort, unknownPort int, scannerFailure string, cli func(...string) []byte) {
	t.Helper()
	config := func(side, body string) {
		t.Helper()
		file := filepath.Join(dir, "auto-"+side+".json")
		if e := os.WriteFile(file, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		args := []string{"plugins", "config", "port", "--side", side, "--file", file}
		if side == "remote" {
			args = append([]string{"--server", "port-test"}, args...)
		}
		cli(args...)
	}
	list := func() []*ppb.ForwardState {
		out := new(ppb.ForwardList)
		decode(t, cli("ports", "list"), out)
		return out.Forwards
	}
	project := new(pb.Project)
	projectPath := filepath.Join(dir, "owned-project")
	if e := os.Mkdir(projectPath, 0700); e != nil {
		t.Fatal(e)
	}
	decode(t, cli("--server", "port-test", "projects", "create", "owned", "--path", projectPath), project)
	session := new(pb.Session)
	decode(t, cli("--server", "port-test", "sessions", "create", "owned", "--project", project.Id), session)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		exec.CommandContext(cleanup, "tmux", "-L", fmt.Sprintf("shiki-%d", os.Getuid()), "kill-session", "-t", "shiki-"+session.Id).Run()
	}()
	config("local", fmt.Sprintf(`{"auto_forward":true,"auto_allow_ports":[%d,%d],"fallback_port_on_conflict":true}`, ownedPort, unknownPort))
	// Server policy still permits only the unrelated service. Neither may auto-start.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if got := list(); len(got) != 0 {
			t.Fatal("unknown/nonallowlisted port automatically forwarded", got)
		}
	}
	config("remote", fmt.Sprintf(`{"allow_ports":[%d,%d],"idle_timeout_seconds":30}`, ownedPort, unknownPort))
	var automatic *ppb.ForwardState
	await(t, ctx, func(context.Context) error {
		got := list()
		if len(got) != 1 {
			return errors.New("automatic owned listener not discovered")
		}
		automatic = got[0]
		if !automatic.Spec.Automatic || automatic.Spec.SessionId != session.Id || int(automatic.Spec.RemotePort) != ownedPort || automatic.BoundPort == 0 || len(automatic.Spec.SourceIdentity) != 64 {
			return fmt.Errorf("invalid automatic listener: %s", automatic)
		}
		return nil
	})
	oldID, sourceIdentity := automatic.Spec.Id, automatic.Spec.SourceIdentity
	exchange := func() {
		t.Helper()
		conn, e := net.DialTCP("tcp4", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(automatic.BoundPort)})
		if e != nil {
			t.Fatal(e)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		conn.Write([]byte("automatic"))
		conn.CloseWrite()
		data, e := io.ReadAll(conn)
		if e != nil || !bytes.Equal(data, []byte("owned:automatic")) {
			t.Fatal("automatic tunnel failed", string(data), e)
		}
	}
	exchange()
	manual := new(ppb.ForwardState)
	decode(t, cli("--server", "port-test", "ports", "forward", strconv.Itoa(unknownPort), "--local-port", "0"), manual)
	activeManual, e := net.DialTCP("tcp4", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(manual.BoundPort)})
	if e != nil {
		t.Fatal(e)
	}
	defer activeManual.Close()
	activeManual.SetDeadline(time.Now().Add(25 * time.Second))
	if _, e = activeManual.Write([]byte("manual-survives-scan")); e != nil {
		t.Fatal(e)
	}
	await(t, ctx, func(context.Context) error {
		for _, state := range list() {
			if state.Spec.Id == manual.Spec.Id && state.Connections > 0 {
				return nil
			}
		}
		return errors.New("manual connection not active")
	})
	if e := os.WriteFile(scannerFailure, []byte("fail"), 0600); e != nil {
		t.Fatal(e)
	}
	await(t, ctx, func(context.Context) error {
		got := list()
		if len(got) != 2 {
			return errors.New("scan failure deleted desired intent")
		}
		for _, state := range got {
			if state.Spec.Automatic {
				if state.Spec.Id != oldID || state.BoundPort != 0 {
					return errors.New("scan failure left automatic listener")
				}
			} else if state.Spec.Id != manual.Spec.Id || state.BoundPort == 0 {
				return errors.New("scan failure stopped manual listener")
			}
		}
		return nil
	})
	if e = activeManual.CloseWrite(); e != nil {
		t.Fatal(e)
	}
	manualReply, e := io.ReadAll(activeManual)
	if e != nil || !bytes.Equal(manualReply, []byte("echo:manual-survives-scan")) {
		t.Fatal("scanner failure disrupted manual TCP session", string(manualReply), e)
	}
	activeManual.Close()
	os.Remove(scannerFailure)
	await(t, ctx, func(context.Context) error {
		for _, state := range list() {
			if state.Spec.Id == oldID && state.BoundPort != 0 {
				automatic = state
				return nil
			}
		}
		return errors.New("automatic listener not recovered after scan")
	})
	cli("agent", "stop")
	cli("agent", "start")
	await(t, ctx, func(context.Context) error {
		for _, state := range list() {
			if state.Spec.Id == oldID && state.BoundPort != 0 {
				automatic = state
				return nil
			}
		}
		return errors.New("automatic desired identity not restored")
	})
	exchange()
	cli("ports", "stop", oldID)
	cli("agent", "stop")
	cli("agent", "start")
	deadline = time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		got := list()
		if len(got) != 1 || got[0].Spec.Id != manual.Spec.Id {
			t.Fatal("restart ignored persisted automatic stop suppression", got)
		}
	}
	cli("--server", "port-test", "sessions", "stop-claude", session.Id, "--yes")
	cli("--server", "port-test", "sessions", "restart-claude", session.Id, "--yes")
	await(t, ctx, func(context.Context) error {
		for _, state := range list() {
			if state.Spec.Automatic && state.BoundPort != 0 {
				if state.Spec.Id == oldID || state.Spec.SourceIdentity == sourceIdentity {
					return errors.New("restarted process reused old source identity")
				}
				automatic = state
				return nil
			}
		}
		return errors.New("new owned process was not discovered")
	})
	exchange()
	cli("--server", "port-test", "sessions", "delete", session.Id, "--yes")
	await(t, ctx, func(context.Context) error {
		got := list()
		if len(got) == 1 && got[0].Spec.Id == manual.Spec.Id {
			return nil
		}
		return errors.New("deleted session retained automatic forward")
	})
	cli("ports", "stop", manual.Spec.Id)
	if got := list(); len(got) != 0 {
		t.Fatal("forward cleanup incomplete", got)
	}
}
