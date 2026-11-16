package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"golang.org/x/term"
)

const maxKeyringSecretBytes = 4096

func keyringSecretReader(username string, stdin bool, path string, confirm bool) func(context.Context) ([]byte, error) {
	return func(ctx context.Context) ([]byte, error) {
		var secret []byte
		var err error
		switch {
		case path != "":
			secret, err = readKeyringSecretFile(path, username)
		case stdin:
			if term.IsTerminal(int(os.Stdin.Fd())) {
				return nil, errors.New("--keyring-secret-stdin refuses an echoing terminal; omit it for the hidden terminal prompt")
			}
			info, statErr := os.Stdin.Stat()
			if statErr != nil {
				return nil, statErr
			}
			account, lookupErr := user.Lookup(username)
			if lookupErr != nil {
				return nil, lookupErr
			}
			uid, parseErr := strconv.ParseUint(account.Uid, 10, 32)
			owner, ok := info.Sys().(*syscall.Stat_t)
			if parseErr != nil || !ok || (owner.Uid != 0 && owner.Uid != uint32(uid)) || info.Mode().Perm()&0077 != 0 || (!info.Mode().IsRegular() && info.Mode()&os.ModeNamedPipe == 0 && info.Mode()&os.ModeSocket == 0) {
				return nil, errors.New("secret stdin must be an explicit owned private file or pipe")
			}
			secret, err = readSecretStream(ctx, os.Stdin)
		default:
			tty, openErr := os.OpenFile("/dev/tty", os.O_RDWR, 0)
			if openErr != nil {
				return nil, errors.New("hidden password input requires a terminal; select --keyring-secret-stdin or --keyring-secret-file explicitly")
			}
			defer tty.Close()
			if confirm {
				_, _ = fmt.Fprintln(tty, "Create a password for the browser's encrypted keyring (not your root/SSH password). Save it securely; normal startup uses the enrolled encrypted credential.")
			} else {
				_, _ = fmt.Fprintln(tty, "Enter the existing browser keyring password. Existing keyrings are never reset.")
			}
			_, _ = fmt.Fprintln(tty, "Typing or pasting shows no characters or asterisks. Press Enter when finished.")
			_, _ = fmt.Fprint(tty, "Keyring password (not echoed): ")
			secret, err = client.ReadSecret(ctx, tty, maxKeyringSecretBytes)
			_, _ = fmt.Fprintln(tty)
			if err == nil && confirm {
				_, _ = fmt.Fprint(tty, "Confirm new dedicated keyring password: ")
				second, readErr := client.ReadSecret(ctx, tty, maxKeyringSecretBytes)
				_, _ = fmt.Fprintln(tty)
				if readErr != nil {
					err = readErr
				} else if !bytes.Equal(secret, second) {
					err = errors.New("password confirmation does not match")
				}
				wipeSecret(second)
			}
		}
		if err != nil {
			wipeSecret(secret)
			return nil, err
		}
		secret = bytes.TrimSuffix(secret, []byte("\n"))
		secret = bytes.TrimSuffix(secret, []byte("\r"))
		if len(secret) == 0 || len(secret) > maxKeyringSecretBytes || bytes.IndexAny(secret, "\x00\r\n") >= 0 || !utf8.Valid(secret) {
			wipeSecret(secret)
			return nil, errors.New("keyring password must contain 1..4096 UTF-8 bytes, without NUL or embedded line breaks")
		}
		return secret, nil
	}
}

func wipeSecret(secret []byte) {
	if secret != nil {
		clear(secret[:cap(secret)])
	}
}

func readSecretStream(ctx context.Context, input io.Reader) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result)
	go func() {
		data, err := io.ReadAll(io.LimitReader(input, maxKeyringSecretBytes+2))
		select {
		case done <- result{data, err}:
		case <-ctx.Done():
			wipeSecret(data)
		}
	}()
	select {
	case value := <-done:
		return value.data, value.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func readKeyringSecretFile(path, username string) ([]byte, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errors.New("secret file must have an absolute path without control characters")
	}
	account, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || owner.Nlink != 1 || info.Size() < 1 || info.Size() > maxKeyringSecretBytes+2 || (owner.Uid != 0 && owner.Uid != uint32(uid)) {
		return nil, errors.New("secret input must be an owned 0600 regular single-link file no larger than 4098 bytes")
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		ancestor, err := os.Lstat(parent)
		if err != nil {
			return nil, err
		}
		stat, ok := ancestor.Sys().(*syscall.Stat_t)
		if !ok || !ancestor.IsDir() || (stat.Uid != 0 && stat.Uid != uint32(uid)) || (owner.Uid == 0 && stat.Uid != 0) || (ancestor.Mode().Perm()&0022 != 0 && !(stat.Uid == 0 && ancestor.Mode()&os.ModeSticky != 0)) {
			return nil, errors.New("secret file ancestors must be protected root/target-user directories, without symlinks or foreign owners")
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, maxKeyringSecretBytes+2))
	if err != nil {
		wipeSecret(data)
		return nil, err
	}
	return data, nil
}
