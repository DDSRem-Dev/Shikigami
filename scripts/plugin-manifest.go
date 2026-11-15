//go:build ignore

// Run by build.sh after compiling a plugin; no downloads or execution of the
// resulting plugin are involved. Release signature verification is separate.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

func main() {
	directory := flag.String("dir", "bin", "artifact directory")
	id := flag.String("id", "", "plugin ID")
	version := flag.String("version", "dev", "matching host version")
	goos := flag.String("os", runtime.GOOS, "artifact target OS")
	flag.Parse()
	if err := run(*directory, *id, *version, *goos); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(directory, id, version, goos string) error {
	if goos != "linux" && goos != "darwin" {
		return fmt.Errorf("unsupported artifact target OS %q", goos)
	}
	if id == "region" && goos != "linux" {
		return fmt.Errorf("region artifacts are Linux server-only")
	}
	declaration, err := host.ReadPluginManifest(filepath.Join("plugins", id))
	if err != nil {
		return err
	}
	if id == "browser" && goos == "darwin" {
		declaration.Sides = []string{"client"}
		declaration.Description = "本地安全查看远端受管 Chrome：画面、输入、显式剪贴板与文件操作"
		fields := declaration.ConfigSchema[:0]
		for _, field := range declaration.ConfigSchema {
			if strings.HasPrefix(field.Key, "viewer_") {
				fields = append(fields, field)
			}
		}
		declaration.ConfigSchema = fields
	}
	side := "both"
	if len(declaration.Sides) == 1 {
		side = declaration.Sides[0]
	}
	protocol, err := sdk.BuiltinProtocol(id, side)
	if err != nil {
		return err
	}
	if id == "browser" && goos == "darwin" {
		declaration.Capabilities = protocol.GetCapabilities()
	}
	binary := "shiki-plugin-" + id
	file, err := os.Open(filepath.Join(directory, binary))
	if err != nil {
		return err
	}
	sum := sha256.New()
	_, err = io.Copy(sum, file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	declaration.Version = version
	manifest := host.Manifest{Name: id, Version: version, Executable: binary, SHA256: hex.EncodeToString(sum.Sum(nil)), Side: side, Protocol: protocol, PluginManifest: declaration}
	if err := host.ValidateBuiltinMetadata(manifest); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".manifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), filepath.Join(directory, id+".manifest.json"))
}
