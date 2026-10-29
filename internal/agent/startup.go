package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

const startupOutputLimit = 64 << 10

var errStartupOutputLimit = errors.New("service query exceeds limit")

// startupObserver configuration is immutable after publication. It reads only the standard
// installer-owned service; arbitrary user automation is outside its scope.
type startupObserver struct {
	busy                   atomic.Bool
	cli                    bool
	config, home, platform string
	uid                    uint32
	run                    func(context.Context, string, ...string) ([]byte, error)
	read                   func(string) ([]byte, error)
	readlink               func(string) (string, error)
}

func newStartupObserver(cli bool, configPath string, uid uint32) *startupObserver {
	p := &startupObserver{cli: cli, platform: runtime.GOOS, uid: uid, readlink: os.Readlink}
	if configPath != "" {
		p.config, _ = filepath.Abs(configPath)
	}
	if account, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
		p.home = account.HomeDir
	}
	p.read = func(path string) ([]byte, error) { return readStartupFile(path, uid) }
	p.run = p.command
	return p
}

func (s *Service) GetStartupPolicy(ctx context.Context, _ *pb.Empty) (*apb.StartupPolicy, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok || !s.Registry.hasCapability(identity.FrontendID, "agent.startup.v1") {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "STARTUP_CAPABILITY_REQUIRED")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &apb.StartupPolicy{CliAutoStart: "unknown", Manager: "unknown", ManagedService: "unknown", LoginStart: "unknown", BootStart: "unknown", ReasonCode: "STARTUP_CONFIGURATION_UNAVAILABLE", ObservedAt: time.Now().UnixMilli()}
	if s.startup == nil {
		return result, nil
	}
	p := s.startup
	if !p.busy.CompareAndSwap(false, true) {
		return nil, connect.NewError(connect.CodeResourceExhausted, "STARTUP_OBSERVATION_BUSY")
	}
	defer p.busy.Store(false)
	result.ConfigPath = p.config
	result.CliAutoStart = "disabled"
	if p.cli {
		result.CliAutoStart = "enabled"
	}
	if p.config == "" || !filepath.IsAbs(p.home) {
		return result, nil
	}
	// This also bounds all sequential manager queries together. An earlier caller
	// deadline wins. Child stderr and command output never enter the RPC response.
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	switch p.platform {
	case "linux":
		p.systemd(probe, result)
	case "darwin":
		p.launchd(probe, result)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if probe.Err() != nil {
		result.ManagedService, result.LoginStart, result.BootStart = "unknown", "unknown", "unknown"
		result.ReasonCode = "STARTUP_OBSERVATION_TIMEOUT"
	}
	return result, nil
}

func readStartupFile(path string, uid uint32) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uid || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > startupOutputLimit {
		return nil, errors.New("unsafe service configuration")
	}
	data, err := io.ReadAll(io.LimitReader(file, startupOutputLimit+1))
	if len(data) > startupOutputLimit {
		return nil, errors.New("service configuration exceeds limit")
	}
	return data, err
}

type startupOutput struct{ buffer bytes.Buffer }

func (w *startupOutput) Write(data []byte) (int, error) {
	if len(data) > startupOutputLimit-w.buffer.Len() {
		return 0, errStartupOutputLimit
	}
	return w.buffer.Write(data)
}
func (p *startupObserver) command(ctx context.Context, binary string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "HOME=" + p.home, "XDG_RUNTIME_DIR=/run/user/" + strconv.FormatUint(uint64(p.uid), 10)}
	command.WaitDelay = 100 * time.Millisecond
	var out startupOutput
	command.Stdout, command.Stderr = &out, io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	return out.buffer.Bytes(), nil
}
