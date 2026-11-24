package tmux

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const fallbackShell = "/bin/sh"

var preferredInteractiveShells = []string{
	"/bin/bash",
	"/usr/bin/bash",
}

var defaultAllowedShells = map[string]bool{
	"/bin/bash":     true,
	"/usr/bin/bash": true,
	"/bin/zsh":      true,
	"/usr/bin/zsh":  true,
	"/bin/ksh":      true,
	"/usr/bin/ksh":  true,
	"/bin/ash":      true,
	"/bin/dash":     true,
	"/usr/bin/dash": true,
	"/bin/sh":       true,
	"/usr/bin/sh":   true,
}

var nonInteractiveShellNames = map[string]bool{
	"false":    true,
	"nologin":  true,
	"true":     true,
	"sync":     true,
	"halt":     true,
	"shutdown": true,
}

// InteractiveShell resolves a trusted, executable interactive shell for the
// current daemon OS user. Account entries that default to minimal POSIX sh/dash
// upgrade to bash when available so interactive line editing and arrow keys work.
func InteractiveShell() string {
	passwdData, _ := readBoundedFile("/etc/passwd", 1<<20)
	shellsData, _ := readBoundedFile("/etc/shells", 64<<10)
	return resolveInteractiveShell(passwdData, shellsData, os.Getenv("SHELL"), os.Geteuid(), os.Stat)
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, os.ErrInvalid
	}
	return io.ReadAll(io.LimitReader(file, limit))
}

func resolveInteractiveShell(passwdData, shellsData []byte, envShell string, uid int, stat func(string) (os.FileInfo, error)) string {
	allowed := parseAllowedShells(shellsData)
	valid := func(candidate string) bool {
		if candidate == "" || strings.ContainsAny(candidate, "\x00\r\n\t ") {
			return false
		}
		if !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
			return false
		}
		if nonInteractiveShellNames[filepath.Base(candidate)] {
			return false
		}
		if !allowed[candidate] {
			return false
		}
		info, err := stat(candidate)
		return err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0
	}

	var configured string
	if passwdShell := lookupPasswdShell(passwdData, uid); valid(passwdShell) {
		configured = passwdShell
	} else if valid(envShell) {
		configured = envShell
	}

	if configured != "" && !isMinimalBourneShell(configured) {
		return configured
	}
	for _, preferred := range preferredInteractiveShells {
		if valid(preferred) {
			return preferred
		}
	}
	if configured != "" {
		return configured
	}
	if valid(fallbackShell) {
		return fallbackShell
	}
	return fallbackShell
}

func isMinimalBourneShell(path string) bool {
	base := filepath.Base(path)
	return base == "sh" || base == "dash"
}

func parseAllowedShells(data []byte) map[string]bool {
	allowed := make(map[string]bool, len(defaultAllowedShells))
	for shell := range defaultAllowedShells {
		allowed[shell] = true
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if filepath.IsAbs(line) && filepath.Clean(line) == line && !nonInteractiveShellNames[filepath.Base(line)] {
			allowed[line] = true
		}
	}
	return allowed
}

func lookupPasswdShell(data []byte, uid int) string {
	if len(data) == 0 || uid < 0 {
		return ""
	}
	targetUID := strconv.Itoa(uid)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) != 7 {
			continue
		}
		if fields[2] == targetUID {
			return fields[6]
		}
	}
	return ""
}
