package reconcile

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"
)

// Defaults apply even when there is no ignore file. Users can explicitly
// negate these rules; the internal staging/history namespace is always private.
var defaultRules = []string{".git/", "node_modules/", ".ssh/", ".aws/", ".gnupg/", ".claude/", ".env", ".env.*", "!.env.example", "!.env.sample"}

type rule struct {
	pattern                     string
	negate, anchored, directory bool
}
type Ignore struct{ rules []rule }

// NewIgnore supports gitignore's comments, ! negation, anchored / paths,
// directory-only trailing /, shell wildcards and whole-component **. Ignoring a
// directory prevents traversal; a child negation cannot reopen its parent.
// Input is bounded and malformed patterns are errors, never silently ignored.
func NewIgnore(reader io.Reader) (*Ignore, error) {
	matcher := &Ignore{}
	for _, line := range defaultRules {
		if err := matcher.add(line); err != nil {
			return nil, err
		}
	}
	if reader == nil {
		return matcher, nil
	}
	limited := &io.LimitedReader{R: reader, N: (1 << 20) + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		if err := matcher.add(scanner.Text()); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limited.N == 0 {
		return nil, errors.New("ignore file exceeds 1 MiB")
	}
	return matcher, nil
}

func (m *Ignore) add(line string) error {
	line = strings.TrimSuffix(line, "\r")
	// Git-style unescaped trailing spaces are insignificant.
	for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, "\\ ") {
		line = strings.TrimSuffix(line, " ")
	}
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}
	r := rule{}
	if strings.HasPrefix(line, "!") {
		r.negate = true
		line = line[1:]
	}
	if strings.HasPrefix(line, "/") {
		r.anchored = true
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		r.directory = true
		line = strings.TrimSuffix(line, "/")
	}
	if line == "" || strings.ContainsRune(line, 0) {
		return errors.New("invalid empty ignore pattern")
	}
	for _, part := range strings.Split(line, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid ignore path component")
		}
		if part != "**" {
			if _, err := path.Match(part, ""); err != nil {
				return err
			}
		}
	}
	r.pattern = line
	m.rules = append(m.rules, r)
	return nil
}

// Ignored expects a normalized slash-separated relative path. Invalid paths
// are rejected from traversal, rather than normalized into another location.
func (m *Ignore) Ignored(name string, directory bool) bool {
	if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") {
		return true
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ".shiki-sync-") {
			return true
		}
	}
	for i := 1; i <= len(parts); i++ {
		prefix := strings.Join(parts[:i], "/")
		isDir := i < len(parts) || directory
		ignored := false
		for _, r := range m.rules {
			if r.directory && !isDir {
				continue
			}
			candidate := prefix
			if !r.anchored && !strings.Contains(r.pattern, "/") {
				candidate = parts[i-1]
			}
			if matchComponents(strings.Split(r.pattern, "/"), strings.Split(candidate, "/")) {
				ignored = !r.negate
			}
		}
		if ignored {
			return true
		}
	}
	return false
}

func matchComponents(pattern, parts []string) bool {
	// Memoize ** states to avoid exponential work on attacker-controlled rules.
	type state struct{ p, s int }
	memo, seen := map[state]bool{}, map[state]bool{}
	var match func(int, int) bool
	match = func(p, s int) bool {
		key := state{p, s}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true
		result := false
		switch {
		case p == len(pattern):
			result = s == len(parts)
		case pattern[p] == "**":
			result = match(p+1, s) || s < len(parts) && match(p, s+1)
		case s < len(parts):
			ok, _ := path.Match(pattern[p], parts[s])
			result = ok && match(p+1, s+1)
		}
		memo[key] = result
		return result
	}
	return match(0, 0)
}
