//go:build linux

package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// directory and strict are an in-process filesystem test seam, never CLI
// input. Production callers below always pass fixed root-owned directories.
func resolverSourceServers(directory string, strict bool) ([]string, error) {
	if strict {
		if err := trustedDirectory(directory); err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	if len(entries) > 256 {
		return nil, errors.New("resolver source inventory exceeds supported bound")
	}
	var servers []string
	remaining := 262144
	for _, entry := range entries {
		if entry.Name() == resolverRecord {
			continue
		}
		data, err := readNoFollow(filepath.Join(directory, entry.Name()), 65536, strict)
		if err != nil {
			return nil, err
		}
		remaining -= len(data)
		if remaining < 0 {
			return nil, errors.New("resolver source data exceeds supported bound")
		}
		servers = append(servers, nameservers(data)...)
	}
	return servers, nil
}

const normalExclusiveMetadata = "exclusive=true;private=false;deprecated=false;metric="

type nativeResolverState struct {
	Metadata       string
	OtherExclusive bool
	Peers          FileState
}

func openresolvState() (nativeResolverState, error) {
	state := nativeResolverState{}
	peers := make(map[string][]byte)
	ownExclusive, ownPrivate, ownDeprecated := false, false, false
	var ownMetrics []string
	remaining, count := 262144, 0
	for _, kind := range []string{"interfaces", "exclusive", "private", "metrics", "deprecated"} {
		directory := filepath.Join("/run/resolvconf", kind)
		if _, err := os.Stat(directory); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return state, err
		}
		if err := trustedDirectory(directory); err != nil {
			return state, err
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return state, err
		}
		if len(entries) > 256 {
			return state, errors.New("openresolv metadata inventory exceeds supported bound")
		}
		for _, entry := range entries {
			count++
			if count > 512 {
				return state, errors.New("openresolv ownership inventory exceeds supported bound")
			}
			data, err := readNoFollow(filepath.Join(directory, entry.Name()), 65536, true)
			if err != nil {
				return state, err
			}
			remaining -= len(data)
			if remaining < 0 {
				return state, errors.New("openresolv ownership data exceeds supported bound")
			}
			own := entry.Name() == resolverRecord || strings.HasSuffix(entry.Name(), " "+resolverRecord)
			if own {
				switch kind {
				case "exclusive":
					ownExclusive = true
				case "private":
					ownPrivate = true
				case "deprecated":
					ownDeprecated = true
				case "metrics":
					ownMetrics = append(ownMetrics, entry.Name())
				}
				continue
			}
			if kind == "exclusive" {
				state.OtherExclusive = true
			}
			peers[kind+"/"+entry.Name()] = data
		}
	}
	state.Metadata = fmt.Sprintf("exclusive=%t;private=%t;deprecated=%t;metric=%s", ownExclusive, ownPrivate, ownDeprecated, strings.Join(ownMetrics, ","))
	encoded, err := json.Marshal(peers)
	if err != nil {
		return state, err
	}
	state.Peers = FileState{Kind: "file", Data: encoded}
	return state, nil
}

func validateOpenresolvConfiguration() error {
	data, err := readTrustedResolved("/etc/resolvconf.conf", 65536)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	values := parseAssignments(string(data))
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		switch key {
		case "state_dir":
			if values[key] != "/run/resolvconf" {
				return fail("failed_precondition", "custom/dynamic openresolv state_dir is unsupported; administrator must use /run/resolvconf")
			}
		case "resolv_conf":
			if values[key] != "/etc/resolv.conf" && values[key] != "/run/resolvconf/resolv.conf" {
				return fail("failed_precondition", "custom/dynamic openresolv resolver output is unsupported; administrator must use the standard owned resolver")
			}
		}
	}
	return nil
}

func resolvedLinkEvidence(dns, domains []byte) FileState {
	var output strings.Builder
	for index, data := range [][]byte{dns, domains} {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "Link ") {
				fmt.Fprintf(&output, "%d:%s\n", index, strings.TrimSpace(line))
			}
		}
	}
	return FileState{Kind: "file", Data: []byte(output.String())}
}

func ownsDebianResolverRecord(record, provenance FileState) bool {
	if record.Kind == "absent" {
		return provenance.Kind == "absent"
	}
	return record.Kind == "file" && provenance.Kind == "file" &&
		string(provenance.Data) == digest(record.Data)
}

// Resolved owns its generated files as systemd-resolve, not root. This read-only
// exception is confined to its two fixed outputs; privileged write rules stay
// root-only. Directory and file descriptors prevent symlink replacement races.
func readSystemResolver(manager string) ([]byte, error) {
	if manager != "systemd-resolved" && manager != "systemd-resolved-inactive" {
		return readTrustedResolved("/etc/resolv.conf", 65536)
	}
	alias, err := readState("/etc/resolv.conf")
	if err != nil {
		return nil, err
	}
	target := alias.Target
	if !filepath.IsAbs(target) {
		target = filepath.Join("/etc", target)
	}
	target = filepath.Clean(target)
	if alias.Kind != "symlink" || (target != "/run/systemd/resolve/stub-resolv.conf" && target != "/run/systemd/resolve/resolv.conf") {
		return nil, errors.New("resolved resolver ownership changed")
	}
	account, err := user.Lookup("systemd-resolve")
	if err != nil {
		return nil, err
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return nil, err
	}
	if err := trustedDirectory("/run/systemd"); err != nil {
		return nil, err
	}
	directory, err := unix.Open("/run/systemd/resolve", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(directory)
	if err := trustedResolvedDescriptor(directory, uint32(uid), unix.S_IFDIR); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(directory, filepath.Base(target), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), target)
	defer file.Close()
	if err := trustedResolvedDescriptor(fd, uint32(uid), unix.S_IFREG); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return nil, err
	}
	if len(data) > 65536 {
		return nil, errors.New("resolved resolver output exceeds size limit")
	}
	return data, nil
}

func trustedResolvedDescriptor(fd int, uid, kind uint32) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if (stat.Uid != 0 && stat.Uid != uid) || stat.Mode&0022 != 0 || stat.Mode&unix.S_IFMT != kind {
		return errors.New("resolved output must belong to root or systemd-resolve, with no group/world write access")
	}
	return nil
}
