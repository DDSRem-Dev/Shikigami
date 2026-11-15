//go:build linux

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Password verification never relies on a live already-unlocked collection.
// Only encrypted bytes are copied to a private temporary bus/store; no secret
// items are read, logged, exported, or copied back to the user's keyring.
func verifyKeyringCredential(parent context.Context, location keyringLocation, secret []byte) (failure error) {
	source, before, err := encryptedKeyring(location.File)
	if err != nil {
		return err
	}
	defer source.Close()
	base := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	temporary, err := os.MkdirTemp(base, "shiki-keyring-verify-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(temporary); err != nil {
			failure = errors.Join(failure, errors.New("private encrypted keyring verification copy could not be removed"))
		}
	}()
	clone := keyringLocation{Home: location.Home, Data: filepath.Join(temporary, "data"), Bus: filepath.Join(temporary, "bus"), Control: filepath.Join(temporary, "control"), Run: temporary}
	clone.Directory = filepath.Join(clone.Data, "keyrings")
	clone.File = filepath.Join(clone.Directory, "login.keyring")
	for _, directory := range []string{clone.Directory, clone.Control} {
		if err := PrivateDir(directory); err != nil {
			return err
		}
	}
	copyFile, err := os.OpenFile(clone.File, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(copyFile, hash), io.LimitReader(source, maxKeyringBytes+1))
	syncErr := copyFile.Sync()
	closeErr := copyFile.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || size != before.Size() || size > maxKeyringBytes {
		return errors.New("encrypted keyring changed or could not be copied safely for credential verification")
	}
	expectedHash := hex.EncodeToString(hash.Sum(nil))
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	config := filepath.Join(temporary, "bus.conf")
	if err := writeSecretsBusConfig(config, clone.Bus); err != nil {
		return err
	}
	bus, err := startChild("/usr/bin/dbus-daemon", []string{"--config-file=" + config, "--nofork"}, keyringEnvironment(clone), io.Discard)
	if err != nil {
		return err
	}
	defer stopVerificationChild(bus)
	if err := awaitPath(ctx, clone.Bus, 5*time.Second); err != nil {
		return err
	}
	if err := os.Chmod(clone.Bus, 0600); err != nil {
		return err
	}
	if err := verifyExecutable("/usr/bin/gnome-keyring-daemon"); err != nil {
		return err
	}
	command := keyringCommand(keyringEnvironment(clone), clone.Control, secret, true)
	if err := command.Start(); err != nil {
		return err
	}
	process := &child{command: command, done: make(chan error, 1)}
	go func() { process.done <- command.Wait() }()
	defer stopVerificationChild(process)
	if err := requireUnlockedLogin(ctx, keyringEnvironment(clone), false); err != nil {
		return errors.New("credential did not unlock the isolated encrypted login keyring copy; no live collection was reset or locked")
	}
	// A daemon must not turn a corrupt/wrong-password file into a newly created
	// empty collection and make that look like a successful password check.
	verified, _, err := encryptedKeyring(clone.File)
	if err != nil {
		return err
	}
	verifiedHash := sha256.New()
	_, err = io.Copy(verifiedHash, io.LimitReader(verified, maxKeyringBytes+1))
	verified.Close()
	if err != nil || hex.EncodeToString(verifiedHash.Sum(nil)) != expectedHash {
		return errors.New("verification daemon replaced or rewrote the encrypted source; credential success is not inferred")
	}
	after, err := os.Lstat(location.File)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return errors.New("live encrypted keyring changed during credential verification; retry explicit enrollment")
	}
	return nil
}
