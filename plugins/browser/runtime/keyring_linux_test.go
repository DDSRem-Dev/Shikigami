//go:build linux

package runtime

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdministratorKeyringPolicyIsUIDBoundAndNotAnExecutionInterface(t *testing.T) {
	for _, mode := range []string{"existing-login", "dedicated"} {
		policy, err := decodeKeyringPolicy([]byte(`{"schema_version":1,"uid":1000,"mode":"`+mode+`","allow_provision":false}`), 1000)
		if err != nil || policy.Mode != mode {
			t.Fatal(err)
		}
	}
	for _, data := range []string{`{"schema_version":1,"uid":0,"mode":"dedicated"}`, `{"schema_version":1,"uid":1000,"mode":"arbitrary"}`, `{"schema_version":1,"uid":1000,"mode":"dedicated","command":"evil"}`, `{"schema_version":1,"uid":1000,"mode":"dedicated"} {}`} {
		if _, err := decodeKeyringPolicy([]byte(data), 1000); err == nil {
			t.Fatalf("unsafe policy accepted: %s", data)
		}
	}
}
func TestBootCredentialIsStdinOnlyAndNeverEmptyBackend(t *testing.T) {
	secret := []byte("test-only-private-password")
	command := keyringCommand([]string{"PATH=/usr/bin:/bin"}, "/private/control", secret, false)
	if strings.Contains(strings.Join(command.Args, " "), string(secret)) || strings.Contains(strings.Join(command.Env, " "), string(secret)) {
		t.Fatal("credential placed in argv/environment")
	}
	actual, err := io.ReadAll(command.Stdin)
	if err != nil || !bytes.Equal(actual, secret) {
		t.Fatal("credential did not stay on stdin")
	}
	joined := strings.Join(command.Args, " ")
	if !strings.Contains(joined, "--unlock") || !strings.Contains(joined, "--components=secrets") || strings.Contains(joined, "--password-store=basic") {
		t.Fatal("wrong fixed unlock operation")
	}
}
func TestDedicatedProvisionNeverResetsAnyExistingKeyring(t *testing.T) {
	directory := t.TempDir()
	location := keyringLocation{Directory: directory, File: filepath.Join(directory, "login.keyring")}
	if err := requireAbsentStore(location); err != nil {
		t.Fatal(err)
	}
	original := []byte("do not reset this user's existing collection")
	if err := os.WriteFile(location.File, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := requireAbsentStore(location); err == nil {
		t.Fatal("existing store accepted for provisioning")
	}
	actual, _ := os.ReadFile(location.File)
	if !bytes.Equal(actual, original) {
		t.Fatal("existing keyring was modified")
	}
	if file, _, err := encryptedKeyring(location.File); err == nil {
		file.Close()
		t.Fatal("plaintext keyring accepted")
	}
}
func TestLaunchRecordPinsExecutableImage(t *testing.T) {
	root := t.TempDir()
	spec := Spec{ProfileID: "one", InstanceID: "instance", Token: "token", Unit: "shiki-browser-test", ConfigPath: filepath.Join(root, "runtime.json"), SocketDir: "/tmp/browser-test", ProfileDir: root, Width: 1440, Height: 900, ExecutableSHA256: strings.Repeat("a", 64)}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.ExecutableSHA256 = ""
	if err := spec.Validate(); err == nil {
		t.Fatal("un-pinned pending runtime image accepted")
	}
}
