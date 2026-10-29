package releaseauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type SignOptions struct {
	Archive, ChecksumsPin, PrivateKey, Signature string
	Version, OS, Arch                            string
}

type VerifyOptions struct {
	Archive, Signature, PublicKey, Output string
	Version, OS, Arch                     string
}

// Verified describes only the authenticated private copy, not an installation.
// The public key fingerprint is identification, never a source of trust.
type Verified struct {
	Format               int    `json:"format"`
	Version              string `json:"version"`
	OS                   string `json:"os"`
	Arch                 string `json:"arch"`
	Archive              string `json:"archive"`
	ArchiveSHA256        string `json:"archive_sha256"`
	ArchiveSize          int64  `json:"archive_size"`
	ChecksumsSHA256      string `json:"checksums_sha256"`
	KeyFingerprintSHA256 string `json:"key_fingerprint_sha256"`
}

// Sign authenticates bytes read from fixed descriptors for the archive and
// generated checksums-pin sidecar. The signer must establish that the supplied
// sidecar belongs to its release build; no archive contents are unpacked here.
func Sign(ctx context.Context, opts SignOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := archiveName(opts.Version, opts.OS, opts.Arch)
	if err != nil {
		return err
	}
	if filepath.Base(opts.Archive) != name || opts.Signature == "" {
		return errors.New("canonical archive basename and new signature output are required")
	}
	keyData, keyInfo, err := readInput(ctx, opts.PrivateKey, "private key", MaxMetadataBytes, true)
	if err != nil {
		return err
	}
	defer clear(keyData)
	private, err := parsePrivate(keyData)
	if err != nil {
		return err
	}
	defer clear(private)
	pinData, pinInfo, err := readInput(ctx, opts.ChecksumsPin, "checksums pin", 65, false)
	if err != nil {
		return err
	}
	pin := strings.TrimSuffix(string(pinData), "\n")
	if !digestPattern.MatchString(pin) {
		return errors.New("checksums pin must be one lowercase SHA256 digest")
	}
	archive, archiveInfo, err := openRegular(opts.Archive, "archive", MaxArchiveBytes, false)
	if err != nil {
		return err
	}
	defer archive.Close()
	if err = distinct(keyInfo, pinInfo, archiveInfo); err != nil {
		return err
	}
	hash := sha256.New()
	size, err := transfer(ctx, hash, archive, MaxArchiveBytes)
	if err != nil {
		return fileError("hash archive", err)
	}
	if err = unchanged(archive, archiveInfo); err != nil || size != archiveInfo.Size() {
		return errors.New("archive changed while signing")
	}
	metadata := payload{Version: opts.Version, OS: opts.OS, Arch: opts.Arch, Archive: name,
		ArchiveSHA256: hex.EncodeToString(hash.Sum(nil)), ArchiveSize: size, ChecksumsSHA256: pin}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return errors.New("cannot encode release metadata")
	}
	signature := ed25519.Sign(private, signedMessage(encoded))
	data, err := json.Marshal(envelope{Format: Format, Payload: base64.StdEncoding.EncodeToString(encoded), Signature: base64.StdEncoding.EncodeToString(signature)})
	if err != nil {
		return errors.New("cannot encode signature envelope")
	}
	return createSignature(ctx, opts.Signature, append(data, '\n'))
}

// Verify authenticates the complete metadata with the independently supplied
// key before opening the archive. It hashes and copies the very same stream
// into a newly created private directory. Only a complete, synced receipt
// marks success. It never executes or extracts the authenticated archive.
func Verify(ctx context.Context, opts VerifyOptions) (result Verified, returnErr error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	name, err := archiveName(opts.Version, opts.OS, opts.Arch)
	if err != nil {
		return result, err
	}
	if filepath.Base(opts.Archive) != name || opts.Output == "" {
		return result, errors.New("canonical archive basename and new private output directory are required")
	}
	publicData, publicInfo, err := readInput(ctx, opts.PublicKey, "public key", MaxMetadataBytes, false)
	if err != nil {
		return result, err
	}
	public, fingerprint, err := parsePublic(publicData)
	if err != nil {
		return result, err
	}
	data, signatureInfo, err := readInput(ctx, opts.Signature, "signature envelope", MaxMetadataBytes, false)
	if err != nil {
		return result, err
	}
	if err = distinct(publicInfo, signatureInfo); err != nil {
		return result, err
	}
	metadata, err := authenticate(data, public)
	if err != nil {
		return result, err
	}
	if metadata.Version != opts.Version || metadata.OS != opts.OS || metadata.Arch != opts.Arch {
		return result, errors.New("authenticated release does not match the expected version and platform")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	archive, archiveInfo, err := openRegular(opts.Archive, "archive", MaxArchiveBytes, false)
	if err != nil {
		return result, err
	}
	defer archive.Close()
	if err = distinct(publicInfo, signatureInfo, archiveInfo); err != nil {
		return result, err
	}
	if archiveInfo.Size() != metadata.ArchiveSize {
		return result, errors.New("archive length differs from authenticated metadata")
	}
	return copyVerified(ctx, opts.Output, metadata, fingerprint, archive, func() error { return unchanged(archive, archiveInfo) })
}

// copyVerified is the shared transaction for local and HTTPS input. It only
// accepts authenticated metadata; callers retain ownership of the input stream.
func copyVerified(ctx context.Context, path string, metadata payload, fingerprint string, source io.Reader, finish func() error) (result Verified, returnErr error) {
	output, err := newOutput(path)
	if err != nil {
		return result, err
	}
	defer output.close()
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, output.remove())
			result = Verified{}
		}
	}()
	copy, err := output.create(metadata.Archive)
	if err != nil {
		return result, fileError("private archive copy", err)
	}
	defer copy.Close()
	if err = copy.Chmod(0600); err != nil {
		return result, fileError("private archive mode", err)
	}
	hash := sha256.New()
	size, err := transfer(ctx, io.MultiWriter(copy, hash), source, metadata.ArchiveSize)
	if err != nil {
		return result, fileError("copy archive", err)
	}
	if finish != nil {
		if err = finish(); err != nil {
			return result, err
		}
	}
	if size != metadata.ArchiveSize || hex.EncodeToString(hash.Sum(nil)) != metadata.ArchiveSHA256 {
		return result, errors.New("archive digest or length does not match authenticated metadata")
	}
	if err = copy.Sync(); err != nil {
		return result, fileError("sync archive copy", err)
	}
	if err = copy.Close(); err != nil {
		return result, fileError("close archive copy", err)
	}
	if err = output.writeNew(ctx, "checksums-pin", []byte(metadata.ChecksumsSHA256+"\n")); err != nil {
		return result, err
	}
	result = Verified{Format: Format, Version: metadata.Version, OS: metadata.OS, Arch: metadata.Arch,
		Archive: filepath.Join(output.path, metadata.Archive), ArchiveSHA256: metadata.ArchiveSHA256, ArchiveSize: metadata.ArchiveSize,
		ChecksumsSHA256: metadata.ChecksumsSHA256, KeyFingerprintSHA256: fingerprint}
	receipt, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, errors.New("cannot encode verification receipt")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = output.check(); err != nil {
		return result, err
	}
	if err = output.writeNew(ctx, "receipt.json", append(receipt, '\n')); err != nil {
		return result, err
	}
	if err = syncRoot(output.root); err != nil {
		return result, err
	}
	if err = syncRoot(output.parent); err != nil {
		return result, err
	}
	if err = output.check(); err != nil {
		return result, err
	}
	return result, ctx.Err()
}

func createSignature(ctx context.Context, path string, data []byte) (returnErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return fileError("signature output directory", err)
	}
	defer parent.Close()
	name := filepath.Base(path)
	file, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fileError("new signature output", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fileError("signature output identity", err)
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, removeOwned(parent, name, info))
		}
	}()
	if err = file.Chmod(0600); err == nil {
		_, err = transfer(ctx, file, bytes.NewReader(data), int64(len(data)))
	}
	if err == nil {
		err = file.Sync()
	}
	if err == nil {
		err = file.Close()
	}
	if err != nil {
		return fileError("write signature output", err)
	}
	if err = syncRoot(parent); err != nil {
		return err
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(current, info) || !current.Mode().IsRegular() || current.Mode().Perm() != 0600 {
		return errors.New("signature output path changed")
	}
	return ctx.Err()
}
