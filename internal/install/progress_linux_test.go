//go:build linux

package install

import (
	"bytes"
	"strings"
	"testing"
)

func TestExtensionProgressHidesCommandArgumentsAndOutput(t *testing.T) {
	var output bytes.Buffer
	ctx := WithProgress(t.Context(), &output)
	// No external network or privileged service: a real subprocess emits
	// sensitive-looking output which must stay in the existing bounded capture.
	if err := extensionRun(ctx, "", "/bin/sh", "-c", "printf private-child-output", "private-command-argument"); err != nil {
		t.Fatal(err)
	}
	if text := output.String(); !strings.Contains(text, "Applying system support: done") || strings.Contains(text, "private-") || strings.Contains(text, "/bin/sh") {
		t.Fatalf("unexpected progress output: %q", text)
	}
	for _, command := range []struct {
		binary   string
		args     []string
		expected string
	}{
		{"/usr/bin/apt-get", []string{"-o", "private-source-path", "update"}, "Refreshing package indexes"},
		{"/usr/bin/apt-get", []string{"install", "private-package-name"}, "Installing system packages"},
		{"/usr/bin/apt-get", []string{"download", "private-package-name"}, "Downloading authenticated Google Chrome"},
		{"/usr/sbin/locale-gen", nil, "Generating language locales"},
		{"/usr/bin/systemctl", []string{"restart", "private-service-name"}, "Configuring and checking system services"},
	} {
		label := extensionProgressLabel(command.binary, command.args)
		if !strings.HasPrefix(label, command.expected) || strings.Contains(label, "private-") {
			t.Fatalf("unexpected command label: %q", label)
		}
	}
}
