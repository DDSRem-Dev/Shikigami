package plugin

import (
	"errors"
	"fmt"
	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

var ErrIncompatible = errors.New("incompatible plugin build or protocol")

// ReadInstalledManifest reads an explicitly installed local artifact. The
// stored checksum is the trust input; it is never recomputed to bless a changed
// binary. Installer/release verification owns provenance and signatures.
func ReadInstalledManifest(directory, id, side, build string) (Manifest, error) {
	var manifest Manifest
	if !filepath.IsAbs(directory) || (id != "sync" && id != "port") || (side != "client" && side != "server") {
		return manifest, errors.New("invalid plugin artifact location or side")
	}
	file, err := os.OpenFile(filepath.Join(directory, id+".manifest.json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return manifest, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return manifest, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || (owner.Uid != 0 && owner.Uid != uint32(os.Getuid())) || info.Size() > 64<<10 {
		return manifest, errors.New("unsafe plugin manifest")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil {
		return manifest, err
	}
	if len(data) > 64<<10 {
		return manifest, errors.New("plugin manifest exceeds limit")
	}
	if err = strictjson.Decode(data, &manifest); err != nil {
		return manifest, err
	}
	if manifest.Name != id || manifest.Version != build || (manifest.Side != "both" && manifest.Side != side) {
		return manifest, fmt.Errorf("%w: %w", ErrIncompatible, connect.NewError(connect.CodeFailedPrecondition, "plugin build or side does not match host"))
	}
	if manifest.Executable != "shiki-plugin-"+id || strings.ContainsAny(manifest.Executable, "/\\") {
		return manifest, errors.New("plugin manifest executable must name its adjacent binary")
	}
	contract, err := sdk.BuiltinProtocol(id, side)
	if err != nil {
		return manifest, err
	}
	if _, err := version.Negotiate(contract, manifest.Protocol, build); err != nil {
		return manifest, fmt.Errorf("%w: %w", ErrIncompatible, err)
	}
	manifest.Protocol = contract
	manifest.Executable = filepath.Join(directory, manifest.Executable)
	manifest.Side = side
	return manifest, nil
}
