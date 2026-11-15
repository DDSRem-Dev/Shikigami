package session

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
)

const environmentBucket = "session.environment"

// LaunchEnvironment is a nonsecret, allowlisted process-start snapshot. It is
// separate from session configuration snapshots and never contains credentials.
// Automatic crash recovery reuses it; only new sessions and explicit restarts
// ask the host for the current environment.
type LaunchEnvironment struct {
	Revision string            `json:"revision,omitempty"`
	Values   map[string]string `json:"values"`
}

var localeEnvironment = []string{
	"TZ", "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_NUMERIC", "LC_TIME",
	"LC_COLLATE", "LC_MONETARY", "LC_MESSAGES", "LC_PAPER", "LC_NAME",
	"LC_ADDRESS", "LC_TELEPHONE", "LC_MEASUREMENT", "LC_IDENTIFICATION",
}

func validEnvironment(value *LaunchEnvironment) error {
	if value == nil {
		return nil
	}
	if len(value.Revision) > 256 || strings.ContainsRune(value.Revision, '\x00') {
		return errors.New("invalid environment revision")
	}
	for name, text := range value.Values {
		allowed := false
		for _, field := range localeEnvironment {
			if name == field && name != "LC_ALL" {
				allowed = true
				break
			}
		}
		if !allowed || len(text) > 4096 || strings.ContainsRune(text, '\x00') {
			return errors.New("invalid process locale environment")
		}
	}
	return nil
}

func (m *Manager) launchCommand(ctx context.Context, id string, argv []string, refresh bool) ([]string, error) {
	if m.opts.Environment == nil {
		return argv, nil
	}
	var environment *LaunchEnvironment
	if refresh {
		if m.opts.Environment != nil {
			var err error
			environment, err = m.opts.Environment(ctx)
			if err != nil {
				return nil, err
			}
		}
	} else {
		state, err := m.store.View()
		if err != nil {
			return nil, err
		}
		value := new(LaunchEnvironment)
		if err := state.Get(environmentBucket, id, value); err == nil {
			environment = value
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := validEnvironment(environment); err != nil {
		return nil, err
	}
	if refresh {
		if environment == nil {
			state, err := m.store.View()
			if err != nil {
				return nil, err
			}
			if _, exists := state.Buckets[environmentBucket][id]; !exists {
				return argv, nil
			}
		}
		if err := m.store.Update(func(state *storage.Snapshot) error {
			if environment == nil {
				delete(state.Buckets[environmentBucket], id)
				return nil
			}
			return state.Put(environmentBucket, id, environment)
		}); err != nil {
			return nil, err
		}
	}
	if environment == nil {
		return argv, nil
	}
	// GNU env executes this argv directly. Nothing is evaluated as a shell
	// fragment, and neither tmux's global nor session environment is mutated.
	command := make([]string, 0, 2*len(localeEnvironment)+len(environment.Values)+len(argv)+2)
	command = append(command, "/usr/bin/env")
	for _, name := range localeEnvironment {
		command = append(command, "-u", name)
	}
	command = append(command, "--")
	names := make([]string, 0, len(environment.Values))
	for name := range environment.Values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		command = append(command, name+"="+environment.Values[name])
	}
	if len(argv) == 0 {
		command = append(command, "/bin/sh")
	} else {
		command = append(command, argv...)
	}
	return command, nil
}
