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

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

func main() {
	directory := flag.String("dir", "bin", "artifact directory")
	id := flag.String("id", "", "plugin ID")
	version := flag.String("version", "dev", "matching host version")
	flag.Parse()
	if err := run(*directory, *id, *version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(directory, id, version string) error {
	protocol, err := sdk.BuiltinProtocol(id, "both")
	if err != nil {
		return err
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
	manifest := host.Manifest{Name: id, Version: version, Executable: binary, SHA256: hex.EncodeToString(sum.Sum(nil)), Side: "both", Protocol: protocol}
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
