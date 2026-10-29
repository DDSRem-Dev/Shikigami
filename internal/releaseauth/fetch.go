package releaseauth

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	fetchTimeout     = 10 * time.Minute
	signatureTimeout = 15 * time.Second
	headerTimeout    = 10 * time.Second
	maxHeaderBytes   = 64 << 10
)

type FetchOptions struct {
	ArchiveURL, SignatureURL, PublicKey, Output string
	Version, OS, Arch                           string
}

// Fetch authenticates metadata before downloading archive bytes into a new
// private output. Both URLs are explicit HTTPS endpoints; trust comes from the
// independently supplied public key, never a key or checksum fetched alongside
// the archive. No redirect, extraction, execution or installation is performed.
func Fetch(ctx context.Context, opts FetchOptions) (Verified, error) {
	client := fetchClient(nil)
	defer client.CloseIdleConnections()
	return fetch(ctx, opts, client)
}

// roots is private test injection. Production always uses system trust and a
// dedicated transport, without ambient cookie jars, auth headers or proxies.
func fetchClient(roots *x509.CertPool) *http.Client {
	transport := &http.Transport{
		DialContext:            (&net.Dialer{Timeout: headerTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		TLSHandshakeTimeout:    headerTimeout,
		ResponseHeaderTimeout:  headerTimeout,
		MaxResponseHeaderBytes: maxHeaderBytes,
		DisableCompression:     true,
		MaxConnsPerHost:        1,
		MaxIdleConns:           2,
		MaxIdleConnsPerHost:    1,
		IdleConnTimeout:        30 * time.Second,
		ForceAttemptHTTP2:      true,
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func fetch(ctx context.Context, opts FetchOptions, client *http.Client) (Verified, error) {
	var empty Verified
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if _, err := archiveName(opts.Version, opts.OS, opts.Arch); err != nil {
		return empty, err
	}
	if strings.EqualFold(opts.Version, "latest") || opts.Output == "" {
		return empty, errors.New("fixed release version and new private output directory are required")
	}
	archiveURL, err := fetchURL(opts.ArchiveURL)
	if err != nil {
		return empty, err
	}
	signatureURL, err := fetchURL(opts.SignatureURL)
	if err != nil {
		return empty, err
	}
	publicData, _, err := readInput(ctx, opts.PublicKey, "public key", MaxMetadataBytes, false)
	if err != nil {
		return empty, err
	}
	public, fingerprint, err := parsePublic(publicData)
	if err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	data, err := fetchSignature(ctx, client, signatureURL)
	if err != nil {
		return empty, err
	}
	metadata, err := authenticate(data, public)
	if err != nil {
		return empty, err
	}
	if metadata.Version != opts.Version || metadata.OS != opts.OS || metadata.Arch != opts.Arch {
		return empty, errors.New("authenticated release does not match the expected version and platform")
	}
	response, err := fetchResponse(ctx, client, archiveURL, metadata.ArchiveSize)
	if err != nil {
		return empty, err
	}
	defer response.Body.Close()
	if response.ContentLength >= 0 && response.ContentLength != metadata.ArchiveSize {
		return empty, errors.New("archive length differs from authenticated metadata")
	}
	return copyVerified(ctx, opts.Output, metadata, fingerprint, sanitizedBody{ctx, response.Body}, nil)
}

func fetchURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawFragment != "" || strings.Contains(raw, "#") {
		return nil, errors.New("release URLs must be absolute HTTPS URLs without credentials or fragments")
	}
	return u, nil
}

func fetchSignature(ctx context.Context, client *http.Client, endpoint *url.URL) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, signatureTimeout)
	defer cancel()
	response, err := fetchResponse(ctx, client, endpoint, MaxMetadataBytes)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var data bytes.Buffer
	if _, err := transfer(ctx, &data, sanitizedBody{ctx, response.Body}, MaxMetadataBytes); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

func fetchResponse(ctx context.Context, client *http.Client, endpoint *url.URL, limit int64) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("cannot construct HTTPS release request")
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		return nil, downloadError(ctx, err)
	}
	encoding := response.Header.Values("Content-Encoding")
	identity := len(encoding) == 0 || (len(encoding) == 1 && (encoding[0] == "" || strings.EqualFold(encoding[0], "identity")))
	if response.StatusCode != http.StatusOK || response.ContentLength > limit || !identity {
		response.Body.Close()
		return nil, errors.New("release response requires HTTP 200, identity encoding and a bounded body")
	}
	return response, nil
}

type sanitizedBody struct {
	ctx  context.Context
	body io.Reader
}

func (s sanitizedBody) Read(p []byte) (int, error) {
	n, err := s.body.Read(p)
	if err == nil || errors.Is(err, io.EOF) {
		return n, err
	}
	return n, downloadError(s.ctx, err)
}

// Never return url.Error, transport/parser messages, headers or response text:
// signed URLs may carry secrets in their query. Retain only cancellation and
// timeout identity, which callers need to distinguish interruption from failure.
func downloadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return fmt.Errorf("HTTPS release download: %w", context.DeadlineExceeded)
	}
	return errors.New("HTTPS release download failed")
}
