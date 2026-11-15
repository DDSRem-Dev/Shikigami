//go:build linux

package runtime

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func verifyExecutable(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if err := verifyExecutableChain(path, path == resolved); err != nil {
		return err
	}
	if path != resolved {
		return verifyExecutableChain(resolved, true)
	}
	return nil
}

func verifyExecutableChain(path string, executableLeaf bool) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		link := info.Mode()&os.ModeSymlink != 0
		if !ok || stat.Uid != 0 || (!link && info.Mode().Perm()&0022 != 0) || (current == path && executableLeaf && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0)) {
			return fmt.Errorf("browser dependency is not a root-owned immutable executable: %s", path)
		}
		if current == "/" {
			break
		}
	}
	return nil
}

type child struct {
	command *exec.Cmd
	done    chan error
}

func startChild(path string, args, environment []string, output io.Writer) (*child, error) {
	if err := verifyExecutable(path); err != nil {
		return nil, err
	}
	command := exec.Command(path, args...)
	command.Env = environment
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		return nil, err
	}
	process := &child{command: command, done: make(chan error, 1)}
	go func() { process.done <- command.Wait() }()
	return process, nil
}

// Run is entered only by the independent systemd user service. It never
// restarts a child; Chrome exit tears down this instance and requires Start.
func Run(configPath string) (failure error) {
	if os.Getuid() <= 0 {
		return errors.New("refusing root Chrome; use a systemd user service")
	}
	installationLease, err := acquireInstallLease()
	if err != nil {
		return err
	}
	defer installationLease.Close()
	var spec Spec
	if err := ReadJSON(configPath, &spec, MaxControlBody); err != nil {
		return err
	}
	if err := spec.Validate(); err != nil {
		return err
	}
	if spec.ConfigPath != configPath {
		return errors.New("runtime configuration path mismatch")
	}
	for _, path := range []string{spec.SocketDir, spec.ProfileDir, filepath.Join(spec.ProfileDir, "chrome"), filepath.Join(spec.ProfileDir, "uploads"), filepath.Join(spec.ProfileDir, "downloads")} {
		if err := PrivateDir(path); err != nil {
			return err
		}
	}
	lock, err := os.OpenFile(filepath.Join(spec.SocketDir, "runtime.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("browser instance already owned")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	defer func() {
		if failure == nil {
			return
		}
		reason := failure.Error()
		if len(reason) > 4096 {
			reason = reason[:4096]
		}
		if err := AtomicJSON(filepath.Join(spec.SocketDir, "exit.json"), Snapshot{ProfileID: spec.ProfileID, InstanceID: spec.InstanceID, State: "crashed", Reason: reason, RegionRevision: spec.RegionRevision}, MaxControlBody); err != nil {
			failure = errors.Join(failure, fmt.Errorf("persisting private runtime exit reason: %w", err))
		}
	}()
	actualDigest, err := executableDigest("/proc/self/exe")
	if err != nil {
		return err
	}
	if actualDigest != spec.ExecutableSHA256 {
		return errors.New("runtime image does not match immutable launch record; maintenance won the pending-start race, use a new explicit start")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	environment, err := launchEnvironment(spec)
	if err != nil {
		return err
	}
	if err := requireChromePolicy(); err != nil {
		return err
	}
	if err := checkInstalledLocales(ctx, environment); err != nil {
		return err
	}
	if err := configureChrome(spec); err != nil {
		return err
	}
	if err := configureIME(spec.SocketDir); err != nil {
		return err
	}
	var children []*child
	defer func() {
		deadline := time.NewTimer(8 * time.Second)
		defer deadline.Stop()
		// Chrome first, with its X display/IME/bus still alive, so an explicit
		// stop gives its persistent cookie store a graceful shutdown window.
		for i := len(children) - 1; i >= 0; i-- {
			process := children[i]
			_ = process.command.Process.Signal(syscall.SIGTERM)
			select {
			case <-process.done:
			case <-deadline.C:
				for _, remaining := range children {
					_ = remaining.command.Process.Kill()
				}
				return
			}
		}
	}()
	start := func(path string, args ...string) (*child, error) {
		process, err := startChild(path, args, environment, os.Stderr)
		if err == nil {
			children = append(children, process)
		}
		return process, err
	}
	if _, err := start("/usr/bin/dbus-daemon", "--session", "--nofork", "--address=unix:path="+filepath.Join(spec.SocketDir, "dbus.sock")); err != nil {
		return err
	}
	if err := awaitPath(ctx, filepath.Join(spec.SocketDir, "dbus.sock"), 5*time.Second); err != nil {
		return err
	}
	display, authPath, err := xAuthorization(spec.SocketDir)
	if err != nil {
		return err
	}
	environment = append(environment, "DISPLAY="+display, "XAUTHORITY="+authPath)
	vncPath := filepath.Join(spec.SocketDir, "vnc.sock")
	if _, err := start("/usr/bin/Xtigervnc", display, "-geometry", fmt.Sprintf("%dx%d", spec.Width, spec.Height), "-depth", "24", "-auth", authPath, "-nolisten", "tcp", "-rfbport", "-1", "-rfbunixpath", vncPath, "-rfbunixmode", "0600", "-SecurityTypes", "None", "-localhost", "yes", "-AlwaysShared", "-NeverShared=0", "-DisconnectClients=0", "-MaxCutText=1048576", "-AcceptCutText=1", "-SendCutText=1"); err != nil {
		return err
	}
	if err := awaitPath(ctx, vncPath, 10*time.Second); err != nil {
		return err
	}
	if _, err := start("/usr/bin/tigervncconfig", "-nowin"); err != nil {
		return err
	}
	if _, err := start("/usr/bin/openbox", "--config-file", filepath.Join(spec.SocketDir, "openbox.xml")); err != nil {
		return err
	}
	dependencyVersions, err := dependencyVersions(ctx)
	if err != nil {
		return err
	}
	chromeEnvironment, err := checkedChromeEnvironment(ctx, environment)
	if err != nil {
		return err
	}
	imeEnvironment := append([]string(nil), environment...)
	for _, entry := range chromeEnvironment {
		if strings.HasPrefix(entry, "DBUS_SESSION_BUS_ADDRESS=") {
			imeEnvironment = replaceEnvironment(imeEnvironment, "DBUS_SESSION_BUS_ADDRESS", strings.TrimPrefix(entry, "DBUS_SESSION_BUS_ADDRESS="))
			break
		}
	}
	ime, err := startChild("/usr/bin/fcitx5", []string{"-D"}, imeEnvironment, os.Stderr)
	if err != nil {
		return err
	}
	children = append(children, ime)
	if err := verifyExecutable(ChromeBinary); err != nil {
		return err
	}
	version, err := command(ctx, ChromeBinary, "--version")
	if err != nil {
		return err
	}
	readInput, writeInput, err := os.Pipe()
	if err != nil {
		return err
	}
	defer writeInput.Close()
	readOutput, writeOutput, err := os.Pipe()
	if err != nil {
		readInput.Close()
		return err
	}
	defer readOutput.Close()
	chrome := exec.Command(ChromeBinary, chromeArguments(spec)...)
	chrome.Env = chromeEnvironment
	chrome.ExtraFiles = []*os.File{readInput, writeOutput}
	chromeLog := newChromeProblems()
	chrome.Stdout, chrome.Stderr = io.Discard, chromeLog
	if err := chrome.Start(); err != nil {
		readInput.Close()
		writeOutput.Close()
		return err
	}
	readInput.Close()
	writeOutput.Close()
	chromeChild := &child{command: chrome, done: make(chan error, 1)}
	children = append(children, chromeChild)
	go func() { chromeChild.done <- chrome.Wait() }()
	cdp := newCDP(writeInput, readOutput)
	defer cdp.Close()
	ready, readyCancel := context.WithTimeout(ctx, 15*time.Second)
	_, err = cdp.call(ready, "", "Browser.getVersion", nil)
	readyCancel()
	if err != nil {
		return fmt.Errorf("Chrome did not initialize sandbox/CDP (%s): %w", chromeLog.summary(), err)
	}
	select {
	case <-chromeLog.failure:
		return errors.New("Chrome reported OS secret storage failure; refusing a plaintext fallback")
	default:
	}
	snapshot := Snapshot{ProfileID: spec.ProfileID, InstanceID: spec.InstanceID, State: "running", RegionRevision: spec.RegionRevision, ChromeVersion: strings.TrimSpace(string(version)), Warnings: []string{"Cookies persist; passwords and Chrome sync are disabled. OS keyring availability is not bypassed.", "Ctrl+Space toggles the server IME; Ctrl+Shift+Space cycles Chinese Pinyin/Japanese Mozc; candidates render in the remote display.", "Profile file pickers run as the daemon UID, not inside a whole-machine file sandbox."}}
	snapshot.Warnings = append(snapshot.Warnings, dependencyVersions...)
	listener, err := net.Listen("unix", filepath.Join(spec.SocketDir, "control.sock"))
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(filepath.Join(spec.SocketDir, "control.sock"), 0600); err != nil {
		return err
	}
	defer os.Remove(filepath.Join(spec.SocketDir, "control.sock"))
	guarded := peerListener{Listener: listener}
	var diagnose sync.Mutex
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if request.Method != http.MethodPost || request.Host != "browser-runtime" || subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+spec.Token)) != 1 {
			http.Error(writer, "forbidden", http.StatusForbidden)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/status":
			_ = json.NewEncoder(writer).Encode(snapshot)
		case "/diagnose":
			request.Body = http.MaxBytesReader(writer, request.Body, MaxControlBody)
			var input epb.DiagnosticRequest
			decoder := json.NewDecoder(request.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil || input.ProfileId != spec.ProfileID {
				http.Error(writer, "invalid diagnostic scope", http.StatusBadRequest)
				return
			}
			if !diagnose.TryLock() {
				http.Error(writer, "diagnostic busy", http.StatusTooManyRequests)
				return
			}
			defer diagnose.Unlock()
			report, err := cdp.diagnose(request.Context(), &input)
			if err != nil {
				http.Error(writer, "target Chrome diagnostics failed", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(writer).Encode(report)
		default:
			http.NotFound(writer, request)
		}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 90 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(guarded) }()
	defer server.Close()
	// One read per child, including early startup failures; do not spin-restart.
	exit := make(chan error, len(children))
	for _, process := range children {
		go func(process *child) {
			select {
			case failure := <-process.done:
				process.done <- failure
				exit <- failure
			case <-ctx.Done():
			}
		}(process)
	}
	select {
	case <-ctx.Done():
		return nil
	case <-chromeLog.failure:
		return errors.New("Chrome reported OS secret storage failure; manual restart after keyring repair is required")
	case err := <-serveDone:
		return err
	case err := <-exit:
		select {
		case <-chromeLog.failure:
			return errors.New("Chrome reported OS secret storage failure; manual restart after keyring repair is required")
		default:
		}
		if err == nil {
			return errors.New("browser dependency exited; manual start required")
		}
		return fmt.Errorf("browser dependency crashed (no automatic restart): %w", err)
	}
}

type peerListener struct{ net.Listener }

func (listener peerListener) Accept() (net.Conn, error) {
	for {
		connection, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if verifyPeer(connection) == nil {
			return connection, nil
		}
		connection.Close()
	}
}

func awaitPath(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Lstat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("browser dependency socket not ready")
		case <-tick.C:
		}
	}
}

func xAuthorization(directory string) (string, string, error) {
	var random [18]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", "", err
	}
	number := 100 + int(binary.BigEndian.Uint16(random[:2]))%20000
	for range 100 {
		if _, err := os.Lstat(fmt.Sprintf("/tmp/.X%d-lock", number)); errors.Is(err, os.ErrNotExist) {
			break
		}
		number++
	}
	display := ":" + strconv.Itoa(number)
	path := filepath.Join(directory, "Xauthority")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	// FamilyWild is private to this authority file; X server still requires
	// the random MIT cookie. No -ac or TCP X listener is used.
	if err := binary.Write(writer, binary.BigEndian, uint16(65535)); err != nil {
		return "", "", err
	}
	for _, field := range [][]byte{nil, []byte(strconv.Itoa(number)), []byte("MIT-MAGIC-COOKIE-1"), random[2:]} {
		if err := binary.Write(writer, binary.BigEndian, uint16(len(field))); err != nil {
			return "", "", err
		}
		if _, err := writer.Write(field); err != nil {
			return "", "", err
		}
	}
	if err := writer.Flush(); err != nil {
		return "", "", err
	}
	if err := file.Sync(); err != nil {
		return "", "", err
	}
	return display, path, nil
}

func launchEnvironment(spec Spec) ([]string, error) {
	environment := []string{"PATH=/usr/bin:/bin", "HOME=" + os.Getenv("HOME"), "USER=" + os.Getenv("USER"), "XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR"), "XDG_CONFIG_HOME=" + filepath.Join(spec.SocketDir, "config"), "XDG_DATA_HOME=" + filepath.Join(spec.SocketDir, "data"), "XDG_CACHE_HOME=" + filepath.Join(spec.SocketDir, "cache"), "DBUS_SESSION_BUS_ADDRESS=unix:path=" + filepath.Join(spec.SocketDir, "dbus.sock"), "GTK_IM_MODULE=fcitx", "QT_IM_MODULE=fcitx", "XMODIFIERS=@im=fcitx"}
	for _, key := range []string{"LANG", "LANGUAGE", "TZ", "LC_ALL", "LC_ADDRESS", "LC_COLLATE", "LC_CTYPE", "LC_IDENTIFICATION", "LC_MEASUREMENT", "LC_MESSAGES", "LC_MONETARY", "LC_NAME", "LC_NUMERIC", "LC_PAPER", "LC_TELEPHONE", "LC_TIME"} {
		if value := os.Getenv(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	if spec.Region != nil {
		if spec.Region.Timezone != "" {
			environment = replaceEnvironment(environment, "TZ", spec.Region.Timezone)
		}
		if spec.Region.Locale != "" {
			for index, entry := range environment {
				if strings.HasPrefix(entry, "LC_") || strings.HasPrefix(entry, "LANGUAGE=") {
					environment[index] = entry[:strings.IndexByte(entry, '=')+1]
				}
			}
			environment = replaceEnvironment(environment, "LANG", spec.Region.Locale)
		}
		if len(spec.Region.Languages) > 0 {
			language := strings.Join(spec.Region.Languages, ":")
			environment = replaceEnvironment(environment, "LANGUAGE", strings.ReplaceAll(language, "-", "_"))
		}
		if len(spec.Region.Formats) > 0 {
			environment = replaceEnvironment(environment, "LC_ALL", "")
		}
		for key, value := range spec.Region.Formats {
			environment = replaceEnvironment(environment, key, value)
		}
	}
	lang, ctype, all := "", "", ""
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		switch key {
		case "LANG":
			lang = value
		case "LC_CTYPE":
			ctype = value
		case "LC_ALL":
			all = value
		}
	}
	if ctype == "" {
		ctype = lang
	}
	if all != "" {
		ctype = all
	}
	if !UTF8Locale(ctype) {
		return nil, errors.New("server XIM requires an explicit UTF-8 locale: set profile.region.locale (for example C.UTF-8) or apply the administrator region locale plan")
	}
	return environment, nil
}

func configureIME(directory string) error {
	config := filepath.Join(directory, "config", "fcitx5")
	for _, path := range []string{filepath.Join(directory, "config"), config, filepath.Join(directory, "data"), filepath.Join(directory, "cache")} {
		if err := PrivateDir(path); err != nil {
			return err
		}
	}
	profile := "[Groups/0]\nName=Default\nDefault Layout=us\nDefaultIM=pinyin\n\n[Groups/0/Items/0]\nName=keyboard-us\nLayout=\n\n[Groups/0/Items/1]\nName=pinyin\nLayout=\n\n[Groups/0/Items/2]\nName=mozc\nLayout=\n\n[GroupOrder]\n0=Default\n"
	global := "[Hotkey/TriggerKeys]\n0=Control+space\n\n[Hotkey/EnumerateForwardKeys]\n0=Control+Shift+space\n"
	for name, value := range map[string]string{"profile": profile, "config": global} {
		if err := os.WriteFile(filepath.Join(config, name), []byte(value), 0600); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(directory, "openbox.xml"), []byte(`<?xml version="1.0"?><openbox_config xmlns="http://openbox.org/3.4/rc"><focus><focusNew>yes</focusNew></focus><desktops><number>1</number></desktops><applications><application class="Google-chrome"><decor>no</decor><maximized>yes</maximized></application></applications></openbox_config>`), 0600)
}

func configureChrome(spec Spec) error {
	preferences := filepath.Join(spec.ProfileDir, "chrome", "Default")
	if err := PrivateDir(preferences); err != nil {
		return err
	}
	if err := mergePreferences(filepath.Join(preferences, "Preferences"), func(values map[string]any) {
		values["credentials_enable_service"] = false
		object(values, "profile")["password_manager_enabled"] = false
		object(values, "signin")["allowed"] = false
		object(values, "sync")["requested"] = false
		download := object(values, "download")
		download["default_directory"] = filepath.Join(spec.ProfileDir, "downloads")
		download["prompt_for_download"] = false
		download["directory_upgrade"] = true
		if spec.Region != nil && len(spec.Region.Languages) > 0 {
			object(values, "intl")["accept_languages"] = strings.Join(chromeLanguages(spec.Region.Languages), ",")
		}
	}); err != nil {
		return err
	}
	return mergePreferences(filepath.Join(spec.ProfileDir, "chrome", "Local State"), func(values map[string]any) {
		if spec.Region == nil || spec.Region.Dns == nil {
			return
		}
		dns := spec.Region.Dns
		switch dns.ChromeMode {
		case "system":
			object(values, "dns_over_https")["mode"] = "off"
		case "secure", "automatic":
			object(values, "dns_over_https")["mode"] = dns.ChromeMode
			object(values, "dns_over_https")["templates"] = strings.Join(dns.ChromeTemplates, " ")
		}
	})
}
func object(parent map[string]any, key string) map[string]any {
	child, ok := parent[key].(map[string]any)
	if !ok {
		child = make(map[string]any)
		parent[key] = child
	}
	return child
}
func mergePreferences(path string, change func(map[string]any)) error {
	values := make(map[string]any)
	if file, err := OpenStage(filepath.Dir(path), filepath.Base(path)); err == nil {
		data, readErr := io.ReadAll(io.LimitReader(file, (16<<20)+1))
		file.Close()
		if readErr != nil {
			return readErr
		}
		if len(data) > 16<<20 {
			return errors.New("Chrome preferences exceed safe startup bound")
		}
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	change(values)
	return AtomicJSON(path, values, 16<<20)
}
