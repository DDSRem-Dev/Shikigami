package pluginmarket

import "testing"

func TestSemanticVersionValidationAndOrdering(t *testing.T) {
	for _, version := range []string{"1.2.3", "v1.2.3", "0.0.1-beta.9", "1.0.0-beta.10+build.7"} {
		if err := ValidateVersion(version); err != nil {
			t.Errorf("valid version %q: %v", version, err)
		}
	}
	for _, version := range []string{"", "dev", "1", "1.2", "01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-beta..9", "1.2.3+", "1.2.3.4", " 1.2.3"} {
		if err := ValidateVersion(version); err == nil {
			t.Errorf("invalid version %q accepted", version)
		}
		entry := testEntry()
		entry.Version = version
		if err := ValidateEntry(entry); err == nil {
			t.Errorf("invalid signed entry version %q accepted", version)
		}
	}
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.9", "1.0.0-beta.10", "1.0.0-rc.1", "1.0.0", "1.0.1", "2.0.0"}
	for i := 1; i < len(ordered); i++ {
		if CompareVersions(ordered[i-1], ordered[i]) >= 0 || CompareVersions(ordered[i], ordered[i-1]) <= 0 {
			t.Fatalf("incorrect SemVer order: %s / %s", ordered[i-1], ordered[i])
		}
	}
	if CompareVersions("1.2.3+one", "v1.2.3+two") != 0 {
		t.Fatal("build metadata changed SemVer precedence")
	}
}
