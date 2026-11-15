package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	erpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions/extensionsconnect"
	"github.com/spf13/cobra"
)

const browserFileChunkSize = 64 << 10

func (a *app) browserFileCommands() *cobra.Command {
	root := &cobra.Command{Use: "files", Short: "Explicitly exchange files through a profile's private staging area, not arbitrary server paths", Args: cobra.NoArgs}
	var area string
	list := &cobra.Command{Use: "list <profile-id>", Args: cobra.ExactArgs(1), Short: "List flat files in a profile's uploads or downloads staging area", RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateCLIProfileID(args[0]); err != nil {
			return err
		}
		if err := validateCLIBrowserArea(area); err != nil {
			return err
		}
		return a.withExtensionServer(cmd.Context(), func(c *client.Client, serverID string) error {
			result, err := c.Browser(serverID).ListBrowserFiles(cmd.Context(), &epb.BrowserFileRequest{ServerId: serverID, ProfileId: args[0], Area: area})
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	list.Flags().StringVar(&area, "area", "downloads", "Profile staging area: downloads or uploads")
	root.AddCommand(list)

	var uploadName string
	var uploadYes bool
	upload := &cobra.Command{Use: "upload <profile-id> <local-file>", Args: cobra.ExactArgs(2), Short: "Explicitly copy one regular local file into the profile's uploads staging area", RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateCLIProfileID(args[0]); err != nil {
			return err
		}
		path, err := client.ConfigPath(args[1])
		if err != nil {
			return err
		}
		name := uploadName
		if name == "" {
			name = filepath.Base(path)
		}
		if err := validateCLIBrowserFileName(name); err != nil {
			return err
		}
		entry, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !entry.Mode().IsRegular() {
			return errors.New("browser upload requires a regular local file")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("browser upload requires a regular local file")
		}
		return a.withConfirmedExtensionServer(cmd.Context(), "Upload local file "+safeText(path)+" into remote profile "+safeText(args[0])+" as "+safeText(name)+"; this explicitly shares its contents with remote Chrome", uploadYes, func(c *client.Client, serverID string) error {
			result, err := uploadCLIBrowserFile(cmd.Context(), c.Browser(serverID), serverID, args[0], name, a.id(), info.Size(), file)
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	upload.Flags().StringVar(&uploadName, "name", "", "Flat remote staging filename; defaults to the local basename")
	upload.Flags().BoolVar(&uploadYes, "yes", false, "Authorize sharing this file with the specified remote profile")
	root.AddCommand(upload)

	var outputPath, downloadArea string
	var downloadYes, overwrite bool
	download := &cobra.Command{Use: "download <profile-id> <name>", Args: cobra.ExactArgs(2), Short: "Explicitly retrieve a staging file, verifying size and SHA-256 before publishing it locally", RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateCLIProfileID(args[0]); err != nil {
			return err
		}
		if err := validateCLIBrowserFileName(args[1]); err != nil {
			return err
		}
		if err := validateCLIBrowserArea(downloadArea); err != nil {
			return err
		}
		if outputPath == "" {
			return errors.New("browser download requires an explicit --output local path")
		}
		path, err := client.ConfigPath(outputPath)
		if err != nil {
			return err
		}
		label := "Retrieve " + safeText(args[1]) + " from remote profile " + safeText(args[0]) + " into " + safeText(path)
		if overwrite {
			label += "; replace any existing local file only after verification"
		}
		if !overwrite {
			if _, err := os.Lstat(path); err == nil {
				return errors.New("local destination already exists; choose another --output or explicitly authorize --overwrite --yes")
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return a.withConfirmedExtensionServer(cmd.Context(), label, downloadYes, func(c *client.Client, serverID string) error {
			request := &epb.BrowserFileRequest{ServerId: serverID, ProfileId: args[0], Area: downloadArea, Name: args[1]}
			result, err := downloadCLIBrowserFile(cmd.Context(), c.Browser(serverID), request, path, overwrite)
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	download.Flags().StringVar(&outputPath, "output", "", "Required: local destination; files are private (0600) and incomplete transfers are removed")
	download.Flags().StringVar(&downloadArea, "area", "downloads", "Profile staging area: downloads or uploads")
	download.Flags().BoolVar(&overwrite, "overwrite", false, "Explicitly allow replacing an existing local destination after verification")
	download.Flags().BoolVar(&downloadYes, "yes", false, "Authorize retrieving this file from the specified remote profile")
	root.AddCommand(download)
	return root
}

func validateCLIBrowserArea(area string) error {
	if area != "uploads" && area != "downloads" {
		return errors.New("--area must be uploads or downloads; arbitrary remote paths are not supported")
	}
	return nil
}

func validateCLIBrowserFileName(name string) error {
	if err := requiredExtensionValue("staging filename", name); err != nil {
		return err
	}
	if len(name) > 255 || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return errors.New("staging filename must be a single flat name, not a path, and at most 255 bytes")
	}
	return nil
}

func uploadCLIBrowserFile(ctx context.Context, remote erpc.BrowserServiceClient, serverID, profileID, name, requestID string, size int64, file io.Reader) (*epb.BrowserFile, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // Cancels an abandoned Connect client stream, including a local read failure.
	stream, err := remote.WriteBrowserFile(ctx)
	if err != nil {
		return nil, err
	}
	chunk := &epb.BrowserFileChunk{ServerId: serverID, ProfileId: profileID, Name: name, RequestId: requestID, TotalSize: size}
	if err := stream.Send(chunk); err != nil {
		return nil, err
	}
	hash := sha256.New()
	buffer := make([]byte, browserFileChunkSize)
	var sent int64
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			sent += int64(count)
			if sent > size {
				return nil, errors.New("local upload file grew during transfer; no completed upload was submitted")
			}
			_, _ = hash.Write(buffer[:count])
			chunk.Data = buffer[:count]
			if err := stream.Send(chunk); err != nil {
				return nil, err
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("reading local upload: %w", readErr)
			}
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if sent != size {
		return nil, errors.New("local upload file changed size during transfer; no completed upload was submitted")
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	chunk.Data = nil
	chunk.Finish = true
	chunk.Sha256 = checksum
	if err := stream.Send(chunk); err != nil {
		return nil, err
	}
	result, err := stream.CloseAndReceive()
	if err != nil {
		return nil, err
	}
	if result.GetProfileId() != profileID || result.GetArea() != "uploads" || result.GetName() != name || result.GetSize() != sent || !strings.EqualFold(result.GetSha256(), checksum) {
		return nil, errors.New("remote upload result does not match the explicitly shared file; inspect profile staging files before retrying")
	}
	return result, nil
}

func downloadCLIBrowserFile(ctx context.Context, remote erpc.BrowserServiceClient, request *epb.BrowserFileRequest, path string, overwrite bool) (*epb.BrowserFile, error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".shiki-browser-download-")
	if err != nil {
		return nil, err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	defer file.Close()
	stream, err := remote.ReadBrowserFile(ctx, request)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	result, err := receiveCLIBrowserFile(request, stream.Receive, file)
	if err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if overwrite {
		err = os.Rename(temporary, path)
	} else {
		// Linking in the same directory atomically refuses an existing file or
		// symlink; a racing local writer cannot turn this into an overwrite.
		err = os.Link(temporary, path)
	}
	if err != nil {
		return nil, fmt.Errorf("publishing verified local download: %w", err)
	}
	return result, nil
}

func receiveCLIBrowserFile(request *epb.BrowserFileRequest, receive func() (*epb.BrowserFileChunk, error), output io.Writer) (*epb.BrowserFile, error) {
	hash := sha256.New()
	var total, received int64
	var expectedHash, checksum string
	metadata, finished := false, false
	for {
		chunk, err := receive()
		if errors.Is(err, io.EOF) {
			if !finished {
				return nil, errors.New("download ended before its verified finish frame; incomplete local file removed")
			}
			break
		}
		if err != nil {
			return nil, err
		}
		if chunk == nil || finished {
			return nil, errors.New("download returned an invalid frame sequence")
		}
		if (chunk.ServerId != "" && chunk.ServerId != request.ServerId) || (chunk.ProfileId != "" && chunk.ProfileId != request.ProfileId) || (chunk.Name != "" && chunk.Name != request.Name) {
			return nil, errors.New("download frame does not match the explicitly requested server, profile, and file")
		}
		if !metadata {
			total, expectedHash = chunk.TotalSize, chunk.Sha256
			if total < 0 || !validCLIBrowserChecksum(expectedHash) {
				return nil, errors.New("download metadata must include a non-negative size and SHA-256 checksum")
			}
			metadata = true
		} else if chunk.TotalSize != 0 && chunk.TotalSize != total {
			return nil, errors.New("download size changed during transfer")
		}
		if len(chunk.Data) > browserFileChunkSize || int64(len(chunk.Data)) > total-received {
			return nil, errors.New("download exceeded its declared size or bounded frame limit")
		}
		if len(chunk.Data) != 0 {
			count, err := output.Write(chunk.Data)
			if err != nil {
				return nil, err
			}
			if count != len(chunk.Data) {
				return nil, io.ErrShortWrite
			}
			_, _ = hash.Write(chunk.Data)
			received += int64(count)
		}
		if chunk.Finish {
			checksum = hex.EncodeToString(hash.Sum(nil))
			if received != total || !strings.EqualFold(expectedHash, checksum) || !strings.EqualFold(chunk.Sha256, checksum) {
				return nil, errors.New("download size or SHA-256 verification failed; incomplete local file removed")
			}
			finished = true
		}
	}
	return &epb.BrowserFile{ProfileId: request.ProfileId, Area: request.Area, Name: request.Name, Size: received, Sha256: checksum}, nil
}

func validCLIBrowserChecksum(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
