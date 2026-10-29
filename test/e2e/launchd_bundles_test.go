//go:build darwin && e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
)

// Supplied bundles must already have been authenticated by the operator. Keep
// their original payloads and identities; only the test harness is compiled.
func launchdLifecycleBundles(t *testing.T, ctx context.Context, repository, fixture string) (string, string, string, string, string, string) {
	t.Helper()
	input := os.Getenv("SHIKI_INSTALL_TEST_BUNDLES")
	if input == "" {
		a, ap := buildLaunchdBundle(t, ctx, repository, fixture, "launchd-test-a")
		b, bp := buildLaunchdBundle(t, ctx, repository, fixture, "launchd-test-b")
		return a, ap, "launchd-test-a", b, bp, "launchd-test-b"
	}
	var bundles []struct {
		Directory string `json:"directory"`
		Version   string `json:"version"`
		Checksums string `json:"checksums_sha256"`
	}
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &bundles); err != nil || len(bundles) != 2 {
		t.Fatal("expected two authenticated prebuilt bundles", err)
	}
	if bundles[0].Version == bundles[1].Version {
		t.Fatal("upgrade acceptance requires distinct bundle versions")
	}
	for _, bundle := range bundles {
		if _, err := install.Verify(bundle.Directory, bundle.Version, bundle.Checksums); err != nil {
			t.Fatal("prebuilt bundle verification", err)
		}
		t.Logf("using verified prebuilt bundle %s; product binaries are not rebuilt", bundle.Version)
	}
	a, b := bundles[0], bundles[1]
	return a.Directory, a.Checksums, a.Version, b.Directory, b.Checksums, b.Version
}
