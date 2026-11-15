//go:build linux

package runtime

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const keyringSecretName = "keyring-password"
const loginCollection = "/org/freedesktop/secrets/collection/login"
const maxKeyringBytes = 16 << 20

var encryptedKeyringMagic = []byte("GnomeKeyring\n\r\x00\n")

type keyringPolicy struct {
	SchemaVersion  int    `json:"schema_version"`
	UID            int    `json:"uid"`
	Mode           string `json:"mode"`
	AllowProvision bool   `json:"allow_provision"`
}
type keyringLocation struct{ Home, Data, Directory, File, Bus, Control, Run string }

func keyringUnit(provision bool) string {
	prefix := "shikigami-browser-keyring-"
	if provision {
		prefix = "shikigami-browser-keyring-provision-"
	}
	return prefix + strconv.Itoa(os.Getuid()) + ".service"
}
func keyringPolicyPath() string {
	return "/etc/shikigami/browser-keyring-" + strconv.Itoa(os.Getuid()) + ".json"
}
func decodeKeyringPolicy(data []byte, uid int) (*keyringPolicy, error) {
	var policy keyringPolicy
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&policy) != nil || policy.SchemaVersion != 1 || policy.UID != uid || (policy.Mode != "existing-login" && policy.Mode != "dedicated") {
		return nil, errors.New("invalid UID-bound administrator browser keyring policy")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing administrator keyring policy data")
	}
	return &policy, nil
}
func loadKeyringPolicy() (*keyringPolicy, error) {
	path := keyringPolicyPath()
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || stat.Uid != 0 || stat.Nlink != 1 || before.Mode().Perm()&0022 != 0 || before.Size() > MaxControlBody {
		return nil, errors.New("administrator keyring policy must be a root-owned immutable ordinary file")
	}
	if err := verifyExecutableChain(path, false); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, errors.New("administrator keyring policy changed during open")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxControlBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxControlBody {
		return nil, errors.New("administrator keyring policy exceeds bound")
	}
	return decodeKeyringPolicy(data, os.Getuid())
}
func locateKeyring(policy *keyringPolicy) (keyringLocation, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return keyringLocation{}, errors.New("systemd user HOME is unavailable")
	}
	run := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	location := keyringLocation{Home: home, Data: filepath.Join(home, ".local", "share"), Bus: filepath.Join(run, "bus"), Control: filepath.Join(run, "keyring"), Run: filepath.Join(run, "shiki-browser-secrets")}
	if policy != nil && policy.Mode == "dedicated" {
		location.Data = filepath.Join(home, ".local", "share", "shikigami", "browser-keyring")
		location.Bus = filepath.Join(location.Run, "bus")
		location.Control = filepath.Join(location.Run, "control")
	}
	location.Directory = filepath.Join(location.Data, "keyrings")
	location.File = filepath.Join(location.Directory, "login.keyring")
	return location, nil
}
func keyringEnvironment(location keyringLocation) []string {
	return []string{"PATH=/usr/bin:/bin", "HOME=" + location.Home, "XDG_RUNTIME_DIR=" + filepath.Join("/run/user", strconv.Itoa(os.Getuid())), "XDG_DATA_HOME=" + location.Data, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + location.Bus, "GNOME_KEYRING_CONTROL=" + location.Control, "LANG=C.UTF-8"}
}

// systemd 255 uses root-owned credential mounts with a named-user POSIX ACL.
// Their group mode bits are the ACL mask, not permission for the owning group.
func credentialACLPrivate(data []byte, mode os.FileMode, owner, uid uint32) bool {
	if len(data) < 4 || (len(data)-4)%8 != 0 || binary.LittleEndian.Uint32(data) != 2 {
		return false
	}
	var seen uint8
	var previousTag, ownerPerm, groupPerm, mask, otherPerm uint16
	var previousUser uint32
	named := false
	for offset := 4; offset < len(data); offset += 8 {
		tag := binary.LittleEndian.Uint16(data[offset:])
		permission := binary.LittleEndian.Uint16(data[offset+2:])
		id := binary.LittleEndian.Uint32(data[offset+4:])
		if permission > 7 || tag < previousTag || (tag != 2 && id != ^uint32(0)) {
			return false
		}
		previousTag = tag
		switch tag {
		case 1: // ACL_USER_OBJ
			if seen&1 != 0 {
				return false
			}
			seen |= 1
			ownerPerm = permission
		case 2: // ACL_USER: only root and this service UID are trusted.
			if (id != 0 && id != uid) || id == owner || (named && id <= previousUser) {
				return false
			}
			named, previousUser = true, id
		case 4: // ACL_GROUP_OBJ
			if seen&2 != 0 {
				return false
			}
			seen |= 2
			groupPerm = permission
		case 16: // ACL_MASK
			if seen&4 != 0 {
				return false
			}
			seen |= 4
			mask = permission
		case 32: // ACL_OTHER
			if seen&8 != 0 {
				return false
			}
			seen |= 8
			otherPerm = permission
		default: // Named groups and unknown ACL entries are never admitted.
			return false
		}
	}
	if seen&11 != 11 || (named && seen&4 == 0) {
		return false
	}
	groupMode := groupPerm
	if seen&4 != 0 {
		groupMode = mask
	}
	return groupPerm&groupMode == 0 && otherPerm == 0 &&
		ownerPerm == uint16(mode.Perm()>>6) &&
		groupMode == uint16(mode.Perm()>>3&7) &&
		otherPerm == uint16(mode.Perm()&7)
}

func checkCredentialAccess(file *os.File, uid uint32, directory bool) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != 0 && stat.Uid != uid) || info.IsDir() != directory ||
		(!directory && (!info.Mode().IsRegular() || stat.Nlink != 1 || info.Size() > 4096)) {
		return errors.New("invalid fixed systemd keyring credential metadata")
	}
	var acl [64]byte // At most the four base entries plus root and service UID.
	if directory {
		_, err := unix.Fgetxattr(int(file.Fd()), "system.posix_acl_default", acl[:])
		if !errors.Is(err, unix.ENODATA) && !errors.Is(err, unix.ENOTSUP) {
			return errors.New("systemd credential directory has an unexpected or unreadable default ACL")
		}
	}
	size, err := unix.Fgetxattr(int(file.Fd()), "system.posix_acl_access", acl[:])
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		if info.Mode().Perm()&0077 == 0 {
			return nil
		}
	} else if err == nil && credentialACLPrivate(acl[:size], info.Mode(), stat.Uid, uid) {
		return nil
	}
	return errors.New("systemd credential access is not restricted to root and the service UID")
}

func readBootCredential(provision bool) ([]byte, error) {
	expected := filepath.Join("/run/credentials", keyringUnit(provision))
	provided := os.Getenv("CREDENTIALS_DIRECTORY")
	if !provision && provided == filepath.Join("/run/credentials", keyringUnit(true)) {
		expected = provided
	}
	if provided != expected {
		return nil, errors.New("keyring action requires its fixed systemd encrypted-credential unit; no arbitrary credential path is accepted")
	}
	folder, err := os.OpenFile(expected, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer folder.Close()
	if err := checkCredentialAccess(folder, uint32(os.Getuid()), true); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(folder.Fd()), keyringSecretName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), keyringSecretName)
	defer file.Close()
	if err := checkCredentialAccess(file, uint32(os.Getuid()), false); err != nil {
		return nil, err
	}
	secret, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return nil, err
	}
	if len(secret) == 0 || len(secret) > 4096 || bytes.IndexByte(secret, 0) >= 0 {
		clear(secret)
		return nil, errors.New("keyring credential must be nonempty, bounded and contain no NUL; no empty/plaintext-keyring fallback")
	}
	return secret, nil
}

func encryptedKeyring(path string) (*os.File, os.FileInfo, error) {
	file, err := OpenStage(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if info.Mode().Perm()&0077 != 0 || info.Size() < int64(len(encryptedKeyringMagic)) || info.Size() > maxKeyringBytes {
		file.Close()
		return nil, nil, errors.New("keyring must be an existing bounded private encrypted GNOME login keyring")
	}
	var header [16]byte
	if _, err := io.ReadFull(file, header[:]); err != nil || !bytes.Equal(header[:], encryptedKeyringMagic) {
		file.Close()
		return nil, nil, errors.New("plaintext or unsupported keyring format is forbidden; existing keyrings are never reset")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, info, nil
}
func keyringCall(ctx context.Context, environment []string, destination, path, method string, args ...string) (string, error) {
	if err := verifyExecutable("/usr/bin/gdbus"); err != nil {
		return "", err
	}
	arguments := []string{"call", "--session", "--dest", destination, "--object-path", path, "--method", method}
	arguments = append(arguments, args...)
	command := exec.CommandContext(ctx, "/usr/bin/gdbus", arguments...)
	command.Env = environment
	var output limitedBuffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", errors.New("keyring private D-Bus operation failed; no secret or tool output is logged")
	}
	return strings.TrimSpace(output.String()), nil
}
func waitSecretService(ctx context.Context, environment []string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, err := keyringCall(ctx, environment, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus.NameHasOwner", "org.freedesktop.secrets")
		if err == nil && response == "(true,)" {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("secret service did not become ready")
		case <-ticker.C:
		}
	}
}
func aliasPath(ctx context.Context, environment []string, alias string) (string, error) {
	result, err := keyringCall(ctx, environment, "org.freedesktop.secrets", "/org/freedesktop/secrets", "org.freedesktop.Secret.Service.ReadAlias", alias)
	if err != nil {
		return "", err
	}
	parts := strings.Split(result, "'")
	if len(parts) != 3 || (!strings.HasPrefix(parts[1], "/org/freedesktop/secrets/collection/") && parts[1] != "/") {
		return "", errors.New("invalid secret-service collection identity")
	}
	return parts[1], nil
}
func requireUnlockedLogin(ctx context.Context, environment []string, requireDefault bool) error {
	if err := waitSecretService(ctx, environment); err != nil {
		return err
	}
	path, err := aliasPath(ctx, environment, "login")
	if err != nil {
		return err
	}
	if path != loginCollection {
		return errors.New("the encrypted GNOME login collection is unavailable; no other keyring is modified")
	}
	if requireDefault {
		path, err = aliasPath(ctx, environment, "default")
		if err != nil {
			return err
		}
		if path != loginCollection {
			return errors.New("default keyring is not the selected login collection; choose dedicated mode rather than resetting user collections")
		}
	}
	locked, err := keyringCall(ctx, environment, "org.freedesktop.secrets", loginCollection, "org.freedesktop.DBus.Properties.Get", "org.freedesktop.Secret.Collection", "Locked")
	if err != nil {
		return err
	}
	if locked != "(<false>,)" {
		return errors.New("encrypted keyring remains locked; supplied boot credential was not accepted")
	}
	return nil
}
func keyringCommand(environment []string, control string, secret []byte, foreground bool) *exec.Cmd {
	mode := "--daemonize"
	if foreground {
		mode = "--foreground"
	}
	command := exec.Command("/usr/bin/gnome-keyring-daemon", "--unlock", "--components=secrets", mode, "--control-directory="+control)
	command.Env = environment
	command.Stdin = bytes.NewReader(secret)
	command.Stdout, command.Stderr = nil, nil
	return command
}

// RunKeyringService accepts only fixed actions from the administrator's systemd
// units. The supplied password is independently verified against an encrypted
// copy, even when the user's live collection is already unlocked.
func RunKeyringService(provision bool) error {
	if os.Getuid() <= 0 {
		return errors.New("keyring actions must run as the configured ordinary daemon UID")
	}
	policy, err := loadKeyringPolicy()
	if err != nil {
		return err
	}
	if policy == nil {
		return errors.New("administrator keyring policy has not been provisioned")
	}
	if provision && (policy.Mode != "dedicated" || !policy.AllowProvision) {
		return errors.New("new keyring provisioning requires explicit dedicated mode and administrator allow_provision")
	}
	secret, err := readBootCredential(provision)
	if err != nil {
		return err
	}
	defer clear(secret)
	location, err := locateKeyring(policy)
	if err != nil {
		return err
	}
	if err := PrivateDir(location.Run); err != nil {
		return err
	}
	lease, err := os.OpenFile(filepath.Join(location.Run, "startup.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lease.Close()
	if err := syscall.Flock(int(lease.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another keyring startup/provision operation is active")
	}
	lifetime, stopSignal := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stopSignal()
	ctx, cancel := context.WithTimeout(lifetime, 40*time.Second)
	defer cancel()
	if provision {
		if err := requireAbsentStore(location); err != nil {
			return err
		}
		if err := PrivateDir(location.Directory); err != nil {
			return err
		}
	} else {
		if _, err := os.Lstat(location.Directory); err != nil {
			return errors.New("encrypted login keyring is missing; normal boot unlock never creates it")
		}
		file, _, err := encryptedKeyring(location.File)
		if err != nil {
			return err
		}
		file.Close()
		if err := verifyKeyringCredential(ctx, location, secret); err != nil {
			return err
		}
	}
	if policy.Mode == "dedicated" {
		created, err := ensureDedicatedSecretsBus(ctx, location)
		if err != nil {
			return err
		}
		if provision && !created {
			return errors.New("private secret bus already exists; stop its unit before provisioning an absent store")
		}
	}
	if err := PrivateDir(location.Control); err != nil {
		return err
	}
	if err := verifyExecutable("/usr/bin/gnome-keyring-daemon"); err != nil {
		return err
	}
	command := keyringCommand(keyringEnvironment(location), location.Control, secret, false)
	if err := command.Start(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = command.Process.Kill() })
	err = command.Wait()
	stop()
	if err != nil {
		return errors.New("GNOME keyring unlock failed; credential and child output are not logged")
	}
	environment := keyringEnvironment(location)
	if err := waitSecretService(ctx, environment); err != nil {
		return err
	}
	if provision {
		if _, err := keyringCall(ctx, environment, "org.freedesktop.secrets", "/org/freedesktop/secrets", "org.freedesktop.Secret.Service.SetAlias", "default", loginCollection); err != nil {
			return err
		}
		if err := verifyKeyringCredential(ctx, location, secret); err != nil {
			return err
		}
	}
	return requireUnlockedLogin(ctx, environment, true)
}

func requireAbsentStore(location keyringLocation) error {
	if info, err := os.Lstat(location.Directory); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("dedicated keyring path is not a directory")
		}
		folder, err := os.Open(location.Directory)
		if err != nil {
			return err
		}
		entries, readErr := folder.ReadDir(1)
		folder.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		if len(entries) != 0 {
			return errors.New("dedicated keyring store is not absent/empty; refusing to reset any keyring")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func waitAdministratorKeyring(parent context.Context, policy *keyringPolicy) error {
	if policy == nil {
		return nil
	}
	if err := verifyExecutable("/usr/bin/systemctl"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		command := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", keyringUnit(false), "--property=LoadState,ActiveState,SubState,Result")
		var output limitedBuffer
		command.Stdout, command.Stderr = &output, io.Discard
		runErr := command.Run()
		properties := make(map[string]string)
		for _, line := range strings.Split(output.String(), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				properties[key] = value
			}
		}
		if properties["LoadState"] == "not-found" {
			return errors.New("administrator encrypted-credential keyring unit is missing; run browser setup")
		}
		if properties["ActiveState"] == "failed" {
			return errors.New("administrator boot keyring unlock failed; inspect the fixed keyring unit and replace its encrypted credential explicitly")
		}
		if runErr == nil && properties["ActiveState"] == "active" && properties["Result"] == "success" {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("administrator keyring unit did not become ready; Chrome was not started and no plaintext backend was selected")
		case <-ticker.C:
		}
	}
}
