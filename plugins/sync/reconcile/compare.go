// Package reconcile contains the deterministic decisions used by a sync worker.
// It performs no I/O: observation errors must never be represented as deletion.
package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

type Action string

const (
	Equal    Action = "equal"
	Upload   Action = "upload"
	Download Action = "download"
	Conflict Action = "conflict"
	Initial  Action = "initial"
)

// Decide compares content and ordinary permissions, never wall-clock times or
// revisions from different machines. A nil baseline means no common ancestor;
// an absent FileVersion is an observed deletion, not an unknown state.
func Decide(base, local, remote *pb.FileVersion) (Action, error) {
	if err := valid(local); err != nil {
		return "", err
	}
	if err := valid(remote); err != nil {
		return "", err
	}
	if base != nil {
		if err := valid(base); err != nil {
			return "", err
		}
	}
	if Same(local, remote) {
		return Equal, nil
	}
	if base == nil {
		return Initial, nil
	}
	if Same(local, base) {
		return Download, nil
	}
	if Same(remote, base) {
		return Upload, nil
	}
	return Conflict, nil
}

// Same deliberately excludes opaque observation revisions. Those revisions
// still participate in conditional commits and conflict generations.
func Same(a, b *pb.FileVersion) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Exists != b.Exists {
		return false
	}
	return !a.Exists || a.ContentHash == b.ContentHash && a.Mode == b.Mode
}

// Generation invalidates a displayed decision whenever either observed
// revision changes, even when the current contents happen to be identical.
func Generation(mappingID, path string, base, local, remote *pb.FileVersion) (string, error) {
	if _, err := Decide(base, local, remote); err != nil {
		return "", err
	}
	type version struct {
		Exists   bool
		Hash     string
		Mode     uint32
		Revision string
	}
	convert := func(v *pb.FileVersion) *version {
		if v == nil {
			return nil
		}
		return &version{v.Exists, v.ContentHash, v.Mode, v.Revision}
	}
	data, err := json.Marshal(struct {
		Mapping, Path       string
		Base, Local, Remote *version
	}{mappingID, path, convert(base), convert(local), convert(remote)})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func valid(v *pb.FileVersion) error {
	if v == nil {
		return errors.New("file observation is missing; cannot infer deletion")
	}
	if !v.Exists {
		if v.ContentHash != "" || v.Mode != 0 {
			return errors.New("deleted file observation contains content")
		}
		return nil
	}
	sum, err := hex.DecodeString(v.ContentHash)
	if err != nil || len(sum) != sha256.Size || hex.EncodeToString(sum) != v.ContentHash || v.Mode & ^uint32(0777) != 0 || v.Revision == "" {
		return errors.New("invalid file observation")
	}
	return nil
}
