//go:build linux

package runtime

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func chromeArguments(spec Spec) []string {
	arguments := []string{"--user-data-dir=" + filepath.Join(spec.ProfileDir, "chrome"), "--no-first-run", "--no-default-browser-check", "--disable-sync", "--password-store=gnome-libsecret", "--ozone-platform=x11", "--gtk-version=3", "--start-maximized", "--remote-debugging-pipe"}
	if spec.Region != nil && len(spec.Region.Languages) > 0 {
		arguments = append(arguments, "--lang="+strings.ReplaceAll(spec.Region.Languages[0], "_", "-"))
	}
	return append(arguments, "about:blank")
}

func replaceEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	for index, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			environment[index] = prefix + value
			return environment
		}
	}
	return append(environment, prefix+value)
}

// Chrome and its owned Fcitx process use the selected keyring bus: GTK input
// modules watch service ownership on the session bus even with IBUS_ADDRESS.
// Fcitx retains the private display and configuration. Never replace an existing
// input-method service or fall back to XIM, which loses rapid Chrome input.
func checkedChromeEnvironment(parent context.Context, environment []string) ([]string, error) {
	environment = append([]string(nil), environment...)
	for key, fallback := range map[string]string{"XDG_CONFIG_HOME": ".config", "XDG_DATA_HOME": ".local/share", "XDG_CACHE_HOME": ".cache"} {
		value := os.Getenv(key)
		if value == "" {
			value = filepath.Join(os.Getenv("HOME"), fallback)
		}
		environment = replaceEnvironment(environment, key, value)
	}
	policy, err := loadKeyringPolicy()
	if err != nil {
		return nil, err
	}
	if err := waitAdministratorKeyring(parent, policy); err != nil {
		return nil, err
	}
	bus := filepath.Join("/run/user", strconv.Itoa(os.Getuid()), "bus")
	var connection net.Conn
	if policy != nil && policy.Mode == "dedicated" {
		location, locateErr := locateKeyring(policy)
		if locateErr != nil {
			return nil, locateErr
		}
		bus = location.Bus
		connection, err = dialPrivate(parent, bus)
	} else {
		connection, err = (&net.Dialer{Timeout: 3 * time.Second}).DialContext(parent, "unix", bus)
		if err == nil {
			err = verifyPeer(connection)
		}
	}
	if connection != nil {
		connection.Close()
	}
	if err != nil {
		return nil, errors.New("selected OS secret-service bus is unavailable; administrator boot unlock must succeed, with no plaintext fallback")
	}
	environment = replaceEnvironment(environment, "DBUS_SESSION_BUS_ADDRESS", "unix:path="+bus)
	environment = replaceEnvironment(environment, "GTK_IM_MODULE", "fcitx")
	environment = replaceEnvironment(environment, "QT_IM_MODULE", "fcitx")
	if err := verifyExecutable("/usr/bin/gdbus"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	call := func(arguments ...string) (string, error) {
		command := exec.CommandContext(ctx, "/usr/bin/gdbus", arguments...)
		command.Env = environment
		var output limitedBuffer
		command.Stdout, command.Stderr = &output, &output
		if command.Run() != nil {
			return "", errors.New("OS secret service is unavailable; unlock/configure the existing user keyring, then explicitly start Chrome again")
		}
		return strings.TrimSpace(output.String()), nil
	}
	owner, err := call("call", "--session", "--dest", "org.freedesktop.DBus", "--object-path", "/org/freedesktop/DBus", "--method", "org.freedesktop.DBus.NameHasOwner", "org.fcitx.Fcitx5")
	if err != nil {
		return nil, err
	}
	if owner != "(false,)" {
		return nil, errors.New("selected keyring bus already has an input-method service; stop the conflicting service explicitly before starting the managed browser")
	}
	alias, err := call("call", "--session", "--dest", "org.freedesktop.secrets", "--object-path", "/org/freedesktop/secrets", "--method", "org.freedesktop.Secret.Service.ReadAlias", "default")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(alias, "'")
	if len(parts) != 3 || !strings.HasPrefix(parts[1], "/org/freedesktop/secrets/collection/") || strings.ContainsAny(parts[1], " \r\n\x00") {
		return nil, errors.New("OS keyring has no persistent default collection; provision an encrypted user keyring explicitly")
	}
	if policy != nil && parts[1] != loginCollection {
		return nil, errors.New("administrator boot keyring policy does not match the live default login collection")
	}
	locked, err := call("call", "--session", "--dest", "org.freedesktop.secrets", "--object-path", parts[1], "--method", "org.freedesktop.DBus.Properties.Get", "org.freedesktop.Secret.Collection", "Locked")
	if err != nil {
		return nil, err
	}
	if locked != "(<false>,)" {
		return nil, errors.New("OS keyring default collection is locked; unlock it in the user session before explicitly starting Chrome (no plaintext fallback)")
	}
	return environment, nil
}

// Never log Chrome's arbitrary page/profile output. Preserve only fixed error
// categories useful for startup diagnosis; cookie/token/page data is discarded.
type chromeProblems struct {
	mu      sync.Mutex
	sandbox bool
	keyring bool
	failure chan struct{}
	once    sync.Once
}

func newChromeProblems() *chromeProblems { return &chromeProblems{failure: make(chan struct{})} }
func (log *chromeProblems) Write(data []byte) (int, error) {
	text := strings.ToLower(string(data))
	keyring := strings.Contains(text, "keyring") || strings.Contains(text, "libsecret") || strings.Contains(text, "password storage") || strings.Contains(text, "freedesktop.secrets")
	failed := strings.Contains(text, "error") || strings.Contains(text, "failed") || strings.Contains(text, "unavailable") || strings.Contains(text, "could not")
	log.mu.Lock()
	if strings.Contains(text, "sandbox") {
		log.sandbox = true
	}
	if keyring {
		log.keyring = true
	}
	log.mu.Unlock()
	if keyring && failed {
		log.once.Do(func() { close(log.failure) })
	}
	return len(data), nil
}
func (log *chromeProblems) summary() string {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.sandbox {
		return "Chrome startup output mentioned sandbox; sandbox was not disabled"
	}
	if log.keyring {
		return "Chrome startup output mentioned OS keyring/libsecret; plaintext backend was not selected"
	}
	return "Chrome startup failed; page/profile output is deliberately not logged"
}
