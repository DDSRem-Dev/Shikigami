//go:build ignore

// release-trust runs from a trusted checkout; never run a verifier obtained
// from the same unverified archive that it is supposed to authenticate.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/DDSRem-Dev/Shikigami/internal/releaseauth"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, out, diagnostic io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, "Usage: release-trust <sign|verify|fetch> [options]\nRun a command with --help for its flags.\nUse a trusted checkout and an independently trusted Ed25519 public key.")
		if len(args) > 1 {
			fmt.Fprintln(diagnostic, "help takes no additional arguments")
			return 2
		}
		return 0
	}
	command := args[0]
	if command != "sign" && command != "verify" && command != "fetch" {
		fmt.Fprintln(diagnostic, "expected sign, verify or fetch")
		return 2
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	var archive, signature, archiveURL, signatureURL string
	if command == "fetch" {
		flags.StringVar(&archiveURL, "archive-url", "", "explicit HTTPS URL of the fixed release archive")
		flags.StringVar(&signatureURL, "signature-url", "", "explicit HTTPS URL of its detached signature")
	} else {
		flags.StringVar(&archive, "archive", "", "local release archive")
		flags.StringVar(&signature, "signature", "", "detached signature file (sign creates a new file)")
	}
	version := flags.String("version", "", "required fixed version")
	goos := flags.String("os", "", "required target OS: linux or darwin")
	arch := flags.String("arch", "", "required target architecture: amd64 or arm64")
	var privateKey, checksumsPin, publicKey, output string
	if command == "sign" {
		flags.StringVar(&privateKey, "private-key", "", "private 0600 PKCS8 Ed25519 PEM key file")
		flags.StringVar(&checksumsPin, "checksums-pin", "", "local release SHA256SUMS digest file")
	} else {
		flags.StringVar(&publicKey, "public-key", "", "independently trusted SPKI Ed25519 PEM public key file")
		flags.StringVar(&output, "output", "", "new private directory for the verified archive, pin and receipt")
	}
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: release-trust %s [options]\n", command)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *version == "" || *goos == "" || *arch == "" ||
		(command != "fetch" && (archive == "" || signature == "")) ||
		(command == "fetch" && (archiveURL == "" || signatureURL == "")) ||
		(command == "sign" && (privateKey == "" || checksumsPin == "")) ||
		(command != "sign" && (publicKey == "" || output == "")) {
		fmt.Fprintln(diagnostic, "all listed options are required; positional arguments are not accepted")
		return 2
	}
	if command == "sign" {
		err := releaseauth.Sign(ctx, releaseauth.SignOptions{
			Archive: archive, ChecksumsPin: checksumsPin, PrivateKey: privateKey,
			Signature: signature, Version: *version, OS: *goos, Arch: *arch,
		})
		if err != nil {
			fmt.Fprintln(diagnostic, "sign:", err)
			return 1
		}
		if err := json.NewEncoder(out).Encode(map[string]string{"signature": signature}); err != nil {
			fmt.Fprintln(diagnostic, "write signing result:", err)
			return 1
		}
		return 0
	}
	var verified releaseauth.Verified
	var err error
	if command == "fetch" {
		verified, err = releaseauth.Fetch(ctx, releaseauth.FetchOptions{
			ArchiveURL: archiveURL, SignatureURL: signatureURL, PublicKey: publicKey,
			Output: output, Version: *version, OS: *goos, Arch: *arch,
		})
	} else {
		verified, err = releaseauth.Verify(ctx, releaseauth.VerifyOptions{
			Archive: archive, Signature: signature, PublicKey: publicKey,
			Output: output, Version: *version, OS: *goos, Arch: *arch,
		})
	}
	if err != nil {
		fmt.Fprintln(diagnostic, command+":", err)
		return 1
	}
	if err := json.NewEncoder(out).Encode(verified); err != nil {
		fmt.Fprintln(diagnostic, "write verification result:", err)
		return 1
	}
	return 0
}
