//go:build linux || darwin

package runtime

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

type childComponent string

const (
	childDBus      childComponent = "dbus"
	childDisplay   childComponent = "display"
	childClipboard childComponent = "clipboard"
	childWindow    childComponent = "window-manager"
	childIME       childComponent = "ime"
	childChrome    childComponent = "chrome"
	childKeyring   childComponent = "keyring"
	childUnknown   childComponent = "unknown"
)

func componentForExecutable(path string) childComponent {
	switch path {
	case "/usr/bin/dbus-daemon":
		return childDBus
	case "/usr/bin/Xtigervnc":
		return childDisplay
	case "/usr/bin/tigervncconfig":
		return childClipboard
	case "/usr/bin/openbox":
		return childWindow
	case "/usr/bin/fcitx5":
		return childIME
	case ChromeBinary:
		return childChrome
	case "/usr/bin/gnome-keyring-daemon":
		return childKeyring
	default:
		return childUnknown
	}
}

// Completion is a broadcast: monitoring and teardown can both await it without
// consuming each other's result. Closing done publishes the immutable outcome.
type child struct {
	command *exec.Cmd
	done    chan struct{}
	exit    childExit
}

type childExit struct {
	component childComponent
	code      int
	signal    syscall.Signal
	waitError bool
}

func observeChild(command *exec.Cmd, component childComponent) *child {
	process := &child{command: command, done: make(chan struct{})}
	go func() {
		err := command.Wait()
		result := childExit{component: component, code: -1, waitError: true}
		var exited *exec.ExitError
		if err == nil || errors.As(err, &exited) {
			if state := command.ProcessState; state != nil {
				if status, ok := state.Sys().(syscall.WaitStatus); ok {
					if status.Exited() {
						result.code, result.waitError = status.ExitStatus(), false
					} else if status.Signaled() {
						result.signal, result.waitError = status.Signal(), false
					}
				}
			}
		}
		process.exit = result
		close(process.done)
	}()
	return process
}

func (result childExit) normalChrome() bool {
	return result.component == childChrome && !result.waitError && result.signal == 0 && result.code == 0
}

func (result childExit) Error() string {
	// Only fixed component labels and numeric status are exposed. In particular,
	// an arbitrary Wait error, argv, environment or child output is never copied.
	component := result.component
	switch component {
	case childDBus, childDisplay, childClipboard, childWindow, childIME, childChrome, childKeyring:
	default:
		component = childUnknown
	}
	status := "wait_error"
	if !result.waitError {
		if result.signal != 0 {
			status = fmt.Sprintf("signal=%d", result.signal)
		} else {
			status = fmt.Sprintf("exit_code=%d", result.code)
		}
	}
	if result.normalChrome() {
		return "browser component chrome exited normally (exit_code=0); manual start required"
	}
	return fmt.Sprintf("browser component %s exited (%s); manual start required", component, status)
}

func awaitRuntimeExit(ctx context.Context, children []*child, secretFailure <-chan struct{}, serveDone <-chan error) (string, error) {
	if ctx.Err() != nil {
		return "", nil
	}
	monitor, cancel := context.WithCancel(ctx)
	defer cancel()
	exits := make(chan *child, len(children))
	for _, process := range children {
		go func(process *child) {
			select {
			case <-process.done:
				exits <- process
			case <-monitor.Done():
			}
		}(process)
	}
	select {
	case <-ctx.Done():
		return "", nil
	case <-secretFailure:
		if ctx.Err() != nil {
			return "", nil
		}
		return "", errors.New("Chrome reported OS secret storage failure; manual restart after keyring repair is required")
	case err := <-serveDone:
		if ctx.Err() != nil {
			return "", nil
		}
		return "", err
	case process := <-exits:
		if ctx.Err() != nil {
			return "", nil
		}
		select {
		case <-secretFailure:
			if ctx.Err() != nil {
				return "", nil
			}
			return "", errors.New("Chrome reported OS secret storage failure; manual restart after keyring repair is required")
		default:
		}
		result := process.exit
		if result.normalChrome() {
			// A completed critical dependency must not be hidden by a simultaneous
			// normal Chrome close. This is an observed exit, not causal ordering.
			for _, other := range children {
				if other == process {
					continue
				}
				select {
				case <-other.done:
					result = other.exit
				default:
				}
				if !result.normalChrome() {
					break
				}
			}
		}
		if ctx.Err() != nil {
			return "", nil
		}
		if result.normalChrome() {
			return result.Error(), nil
		}
		return "", result
	}
}
