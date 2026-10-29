package connection

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestTLSMaterialBoundsAndSpecialFiles(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	keyBytes, err := x509.MarshalPKCS8PrivateKey(server.TLS.Certificates[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		cert: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}),
		key:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}),
	} {
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = LoadTLSCertificate(cert, key); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "configured-ca.pem")
	if err = os.Symlink(cert, link); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadRootCAs(link); err != nil {
		t.Fatalf("explicit CA symlink rejected: %v", err)
	}
	oversized := filepath.Join(dir, "large.pem")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(maxCertificateBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	for _, path := range []string{dir, oversized, "/dev/null"} {
		if _, err = LoadRootCAs(path); err == nil {
			t.Fatalf("unsafe TLS input accepted: %s", path)
		}
	}
	fifo := filepath.Join(dir, "pipe.pem")
	if err = syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"CA", "key"} {
		t.Run(kind, func(t *testing.T) {
			done := make(chan error, 1)
			go func() {
				if kind == "CA" {
					_, err := LoadRootCAs(fifo)
					done <- err
				} else {
					_, err := LoadTLSCertificate(cert, fifo)
					done <- err
				}
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("TLS FIFO accepted")
				}
			case <-time.After(time.Second):
				peer, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0)
				if err == nil {
					peer.Close()
				}
				t.Fatal("TLS file read waited for a pipe writer")
			}
		})
	}
}
