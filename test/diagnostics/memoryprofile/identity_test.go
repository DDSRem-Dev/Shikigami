//go:build memoryprofile

package memoryprofile_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

type processIdentity struct {
	Role       string `json:"role"`
	PID        int    `json:"pid"`
	Created    int64  `json:"created_ms"`
	Executable string `json:"executable"`
}

func currentReady(ctx context.Context) (ready, error) {
	p, err := process.NewProcessWithContext(ctx, int32(os.Getpid()))
	if err != nil {
		return ready{}, err
	}
	created, err := p.CreateTimeWithContext(ctx)
	if err != nil {
		return ready{}, err
	}
	executable, err := p.ExeWithContext(ctx)
	if err != nil {
		return ready{}, err
	}
	return ready{PID: os.Getpid(), Created: created, Executable: executable}, nil
}

func readyIdentities(root string) ([]processIdentity, error) {
	files, err := filepath.Glob(filepath.Join(root, "*.ready.json"))
	if err != nil {
		return nil, err
	}
	var result []processIdentity
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var info ready
		if err := json.Unmarshal(data, &info); err != nil {
			return nil, err
		}
		if info.PID <= 1 || info.Created <= 0 || !filepath.IsAbs(info.Executable) {
			return nil, fmt.Errorf("incomplete process identity: %s", file)
		}
		result = append(result, processIdentity{strings.TrimSuffix(filepath.Base(file), ".ready.json"), info.PID, info.Created, info.Executable})
	}
	return result, nil
}

func recordReadyIdentities(root string) error {
	identities, err := readyIdentities(root)
	if err != nil {
		return err
	}
	directory := os.Getenv("SHIKI_RUN_EVIDENCE_DIR")
	if directory == "" {
		directory = root
	}
	return saveJSON(filepath.Join(directory, "processes.json"), map[string]any{"processes": identities})
}

func checkIdentity(ctx context.Context, want processIdentity) error {
	p, err := process.NewProcessWithContext(ctx, int32(want.PID))
	if err != nil {
		return err
	}
	created, err := p.CreateTimeWithContext(ctx)
	if err != nil {
		return err
	}
	executable, err := p.ExeWithContext(ctx)
	if err != nil {
		return err
	}
	if created != want.Created || executable != want.Executable {
		return fmt.Errorf("%s process identity changed", want.Role)
	}
	return nil
}
