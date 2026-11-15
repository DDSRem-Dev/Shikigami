package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
)

func quickstartExec(ctx context.Context, plan install.Plan, asUser bool, binary string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if asUser {
		account, err := user.Lookup(plan.User)
		if err != nil {
			return nil, err
		}
		uid, err := strconv.ParseUint(account.Uid, 10, 32)
		if err != nil || uid == 0 {
			return nil, errors.New("quickstart refuses root runtime commands")
		}
		gid, err := strconv.ParseUint(account.Gid, 10, 32)
		if err != nil {
			return nil, err
		}
		if account.HomeDir != plan.Home {
			return nil, errors.New("runtime account home changed")
		}
		if os.Geteuid() == 0 {
			cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}
		} else if uint32(os.Geteuid()) != uint32(uid) {
			return nil, errors.New("runtime account differs from installer")
		}
		cmd.Dir = plan.Home
		cmd.Env = []string{"HOME=" + plan.Home, "USER=" + plan.User, "LOGNAME=" + plan.User, "PATH=" + plan.Path, "LANG=C.UTF-8", "XDG_RUNTIME_DIR=/run/user/" + account.Uid}
		if terminal := os.Getenv("TERM"); terminal != "" && !strings.ContainsAny(terminal, "\x00\r\n") {
			cmd.Env = append(cmd.Env, "TERM="+terminal)
		}
	} else {
		cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8", "DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=l"}
	}
	return cmd, nil
}

func quickstartCommand(parent context.Context, plan install.Plan, asUser bool, tty *os.File, stdin io.Reader, binary string, args ...string) error {
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	defer cancel()
	cmd, err := quickstartExec(ctx, plan, asUser, binary, args...)
	if err != nil {
		return err
	}
	if stdin == nil {
		stdin = tty
		if asUser {
			// Interactive frontends must remain in the terminal's foreground
			// process group, otherwise their first read is stopped by SIGTTIN.
			// They own no daemon/plugin resources; cancellation kills this short
			// frontend while the already managed services intentionally survive.
			cmd.SysProcAttr.Setpgid = false
			cmd.Cancel = func() error { return cmd.Process.Kill() }
		}
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, tty, tty
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", binary, err)
	}
	return nil
}

type quickstartBoundedOutput struct {
	bytes.Buffer
	limit int
}

func (w *quickstartBoundedOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.Len() {
		return 0, errors.New("quickstart command output exceeds limit")
	}
	return w.Buffer.Write(p)
}

func quickstartCapture(parent context.Context, plan install.Plan, binary string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	cmd, err := quickstartExec(ctx, plan, true, binary, args...)
	if err != nil {
		return nil, err
	}
	output := &quickstartBoundedOutput{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = output, io.Discard
	err = cmd.Run()
	return output.Bytes(), err
}

func quickstartExit(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 3 {
		return 3
	}
	return 1
}

func quickstartPrivateRead(path string, uid uint32, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || owner.Uid != uid || owner.Nlink != 1 || info.Size() > limit {
		return nil, errors.New("unsafe existing quickstart state; preserve it for administrator review")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = errors.New("quickstart state exceeds limit")
	}
	return data, err
}

func quickstartLinuxPlatform() error {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return errors.New("quickstart requires running systemd")
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = strings.Trim(value, `"'`)
		}
	}
	if (values["ID"] == "debian" && values["VERSION_ID"] == "12") || (values["ID"] == "ubuntu" && values["VERSION_ID"] == "24.04") {
		return nil
	}
	return errors.New("guided daemon setup supports Debian 12 and Ubuntu 24.04; use advanced installation on other systems")
}

func quickstartClaudeScript(ctx context.Context, transport http.RoundTripper) ([]byte, error) {
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.User != nil || len(via) > 5 {
			return errors.New("Claude installer requires bounded HTTPS redirects")
		}
		return nil
	}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://claude.ai/install.sh", nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("official Claude installer returned HTTP %d", response.StatusCode)
	}
	const limit = 1 << 20
	if response.ContentLength > limit {
		return nil, errors.New("official Claude installer exceeds 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > limit {
		return nil, errors.New("official Claude installer is empty or exceeds 1 MiB")
	}
	return data, nil
}
