package connection

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"os"
	"syscall"
)

const maxCertificateBytes = 1 << 20

// TLS paths may be explicit symlinks (for example a system CA bundle). Pin the
// opened descriptor and reject devices/FIFOs before reading; a malformed path
// must not block the agent's connection/state owner waiting for a pipe peer.
func readTLSFile(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxCertificateBytes {
		return nil, errors.New("TLS material must be a nonempty regular file no larger than 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCertificateBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxCertificateBytes {
		return nil, errors.New("TLS material is empty or exceeds 1 MiB")
	}
	return data, nil
}

func LoadRootCAs(path string) (*x509.CertPool, error) {
	data, err := readTLSFile(path)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		return nil, errors.New("invalid CA file")
	}
	return roots, nil
}

func LoadTLSCertificate(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := readTLSFile(certFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	key, err := readTLSFile(keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	defer clear(key)
	return tls.X509KeyPair(cert, key)
}
