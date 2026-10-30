package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func (h *helper) front(ctx context.Context, name string) (*frontend.Client, error) {
	if name == "" {
		name = "default"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, invalid()
	}
	f := h.fronts[name]
	if f == nil {
		return nil, errors.New("frontend not open")
	}
	return f, nil
}
func (h *helper) frontendOp(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		Name string `json:"name"`
	}](r)
	if err != nil {
		return nil, invalid()
	}
	if a.Name == "" {
		a.Name = "default"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, invalid()
	}
	if r.Op == "frontend.close" {
		f := h.fronts[a.Name]
		delete(h.fronts, a.Name)
		if f != nil {
			return map[string]bool{"closed": true}, f.Close()
		}
		return map[string]bool{"closed": true}, nil
	}
	if h.fronts[a.Name] != nil || len(h.fronts) >= 16 {
		return nil, invalid()
	}
	probe, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var f *frontend.Client
	for {
		f, err = frontend.Open(probe, h.config, frontend.Options{})
		if err == nil {
			status, statusErr := f.Agent().GetStatus(probe, &pb.Empty{})
			if statusErr != nil || status.GetState() != "ready" {
				_ = f.Close()
				err = errors.New("agent not ready")
			}
		}
		if err == nil {
			break
		}
		select {
		case <-probe.Done():
			return nil, probe.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	h.fronts[a.Name] = f
	return map[string]string{"frontend_id": f.FrontendID(), "instance_id": f.InstanceID()}, nil
}

var rpcMethods = map[string]map[string]bool{
	"agent":       methodSet("GetStatus GetPreferences SetPreferences ListServers ListMappings PutMapping RemoveMapping GetSyncState ListConflicts ResolveConflict CreateForward StopForward ListForwards ListPlugins ManagePlugin"),
	"file-direct": methodSet("StatFile"),
	"remote":      methodSet("GetSystemInfo ListProjects CreateProject GetProject DeleteProject CreateSession GetSession GetSessionScreen TakeControl DeleteSession"),
}

func methodSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, s := range strings.Fields(s) {
		m[s] = true
	}
	return m
}
func (h *helper) rpc(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		ExpectedDaemonID string          `json:"expected_daemon_id"`
		Frontend         string          `json:"frontend"`
		Service          string          `json:"service"`
		ServerID         string          `json:"server_id"`
		Method           string          `json:"method"`
		Message          json.RawMessage `json:"message"`
	}](r)
	if err != nil || !rpcMethods[a.Service][a.Method] {
		return nil, invalid()
	}
	var client any
	if a.Service == "file-direct" {
		remote, close, err := h.directFiles(ctx, a.Frontend, a.ServerID, a.ExpectedDaemonID)
		if err != nil {
			return nil, err
		}
		defer close()
		client = remote
	} else {
		f, err := h.front(ctx, a.Frontend)
		if err != nil {
			return nil, err
		}
		client = f.Agent()
		if a.Service == "remote" {
			if a.ServerID == "" {
				return nil, invalid()
			}
			client = f.Remote(a.ServerID)
		}
	}

	method := reflect.ValueOf(client).MethodByName(a.Method)
	if !method.IsValid() {
		return nil, invalid()
	}
	typ := method.Type()
	if typ.NumIn() != 2 || typ.NumOut() != 2 {
		return nil, invalid()
	}
	msg, ok := reflect.New(typ.In(1).Elem()).Interface().(proto.Message)
	if !ok {
		return nil, invalid()
	}
	if len(a.Message) == 0 {
		a.Message = json.RawMessage("{}")
	}
	if err = protojson.Unmarshal(a.Message, msg); err != nil {
		return nil, invalid()
	}
	values := method.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(msg)})
	if !values[1].IsNil() {
		return nil, values[1].Interface().(error)
	}
	out := values[0].Interface().(proto.Message)
	data, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}).Marshal(out)
	if err != nil {
		return nil, err
	}
	var result any
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return redact(result), nil
}

type cappedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > 2<<20 {
		b.overflow = true
		return 0, errors.New("output limit")
	}
	_, err := b.Buffer.Write(p)
	return n, err
}
func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			lower := strings.ToLower(k)
			if (strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "url") || lower == "auth_key" || lower == "authkey" || lower == "private_key") && !strings.Contains(lower, "revision") {
				delete(x, k)
			} else {
				x[k] = redact(item)
			}
		}
	case []any:
		for i, item := range x {
			x[i] = redact(item)
		}
	case string:
		if strings.Contains(x, "https://") || strings.Contains(x, "http://") || strings.Contains(x, "tskey-") {
			return "[redacted]"
		}
	}
	return v
}
func (h *helper) command(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		Args []string `json:"args"`
	}](r)
	if err != nil || len(a.Args) == 0 || len(a.Args) > 64 {
		return nil, invalid()
	}
	allowed := methodSet("--version version connect sync plugins")
	binary, config := h.cli, h.config
	if r.Op == "daemon.cli" {
		allowed = methodSet("token")
		binary, config = h.daemon, h.daemonConfig
		if binary == "" || config == "" {
			return nil, invalid()
		}
	}
	if !allowed[a.Args[0]] {
		return nil, invalid()
	}
	if r.Op == "daemon.cli" && (len(a.Args) != 3 || a.Args[0] != "token" || a.Args[1] != "revoke") {
		return nil, invalid()
	}
	if a.Args[0] == "plugins" && (len(a.Args) < 2 || (a.Args[1] != "enable" && a.Args[1] != "disable")) {
		return nil, invalid()
	}
	if a.Args[0] == "sync" && (len(a.Args) < 2 || (a.Args[1] != "map" && a.Args[1] != "pause" && a.Args[1] != "resume")) {
		return nil, invalid()
	}
	for i, arg := range a.Args {
		if arg == "--config" || strings.HasPrefix(arg, "--config=") || arg == "--token" || strings.HasPrefix(arg, "--token=") || strings.HasPrefix(arg, "--token-stdin") || strings.Contains(arg, "tskey-") {
			return nil, invalid()
		}
		for _, name := range []string{"--token-file", "--tls-ca", "--local"} {
			if value, ok := strings.CutPrefix(arg, name+"="); ok {
				if _, err = h.path(value); err != nil {
					return nil, err
				}
			}
		}
		if arg == "--token-file" || arg == "--tls-ca" || arg == "--local" {
			if i+1 >= len(a.Args) {
				return nil, invalid()
			}
			if _, err = h.path(a.Args[i+1]); err != nil {
				return nil, err
			}
		}
	}
	var argv []string
	if r.Op == "cli" {
		argv = []string{"--config", config, "--json"}
		argv = append(argv, a.Args...)
	} else {
		argv = append(append([]string{}, a.Args...), "--config", config)
	}
	cmd := exec.CommandContext(ctx, binary, argv...)
	cmd.Dir = h.root
	cmd.Env = append(os.Environ(), "SHIKI_NO_COLOR=1")
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			exitCode = exit.ExitCode()
		}
	}
	result := map[string]any{"exit_code": exitCode}
	var decoded any
	if json.Unmarshal(stdout.Bytes(), &decoded) == nil {
		result["json"] = redact(decoded)
	} else if err == nil {
		result["stdout_sha256"] = digest(stdout.Bytes())
	}
	if stdout.overflow || stderr.overflow {
		return result, invalid()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, err
}

func ownedBinary(root, binary string) bool {
	rel, err := filepath.Rel(root, binary)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
