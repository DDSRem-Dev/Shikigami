//go:build linux

package helper

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"golang.org/x/sys/unix"
)

func TestNativeResolverSourcesExcludeOnlyOwnedRecord(t *testing.T) {
	directory := t.TempDir()
	for name, data := range map[string]string{
		resolverRecord: "nameserver 1.1.1.1\n", "eth0.dhclient": "nameserver 9.9.9.9\nsearch example.invalid\n",
		"wg0.vpn": "nameserver 10.2.0.1\n",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	servers, err := resolverSourceServers(directory, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(servers, ",") != "9.9.9.9,10.2.0.1" {
		t.Fatalf("unrelated VPN/DHCP evidence was discarded, or old owned record blocked replacement: %v", servers)
	}
	if data, err := os.ReadFile(filepath.Join(directory, "wg0.vpn")); err != nil || string(data) != "nameserver 10.2.0.1\n" {
		t.Fatal("source inspection mutated a peer record")
	}
}

func TestNativeResolverSourceReaderRejectsSymlinks(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(t.TempDir(), "protected")
	if err := os.WriteFile(target, []byte("nameserver 9.9.9.9\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, "malicious.source")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolverSourceServers(directory, false); err == nil {
		t.Fatal("a resolver source symlink escaped the injected directory")
	}
}

func TestResolvedEvidenceSeparatesGlobalAndPerLinkRoutes(t *testing.T) {
	state := resolvedLinkEvidence([]byte("Global: 1.1.1.1\nLink 2 (eth0): 9.9.9.9\n"), []byte("Global: ~.\nLink 3 (wg0): ~private.example\n"))
	if bytes.Contains(state.Data, []byte("Global:")) || !bytes.Contains(state.Data, []byte("Link 2")) || !bytes.Contains(state.Data, []byte("~private.example")) {
		t.Fatalf("global settings were mistaken for sole effective DNS evidence: %s", state.Data)
	}
}

func TestDNSConnectivityProbesNeverDialSpecialAddresses(t *testing.T) {
	for _, address := range []string{"0.1.2.3", "127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "224.0.0.1", "::1", "fc00::1", "fe80::1", "2001:db8::1", "64:ff9b::a00:1"} {
		if publicProbeAddress(netip.MustParseAddr(address)) {
			t.Errorf("privileged probe permitted special address %s", address)
		}
	}
	for _, address := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicProbeAddress(netip.MustParseAddr(address)) {
			t.Errorf("ordinary explicit public probe denied: %s", address)
		}
	}
}

func TestUTCAndInstalledLocaleSpellingAreExplicit(t *testing.T) {
	config, err := NormalizeConfig(&epb.RegionConfig{Country: "US", Timezone: "UTC", Locale: "en_US.utf8", Languages: []string{"en"}})
	if err != nil || config.Locale != "en_US.UTF-8" {
		t.Fatalf("valid explicit UTC/UTF-8 spelling: %v", err)
	}
	if _, err := NormalizeConfig(&epb.RegionConfig{Country: "JP", Timezone: "Local"}); err == nil {
		t.Fatal("process-local timezone alias was accepted as a fixed IANA system target")
	}
}

func TestHelperRejectsProductionRootAndCommandArguments(t *testing.T) {
	for _, args := range [][]string{{"apply", "--root=/tmp"}, {"exec"}, {"guard", "/tmp/state"}} {
		var output bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(`{}`), &output, &output); code != 2 {
			t.Fatalf("unapproved arguments reached privileged initialization: %v code=%d", args, code)
		}
	}
}

func TestNoDNSRequestKeepsApprovedPolicyWithoutReapplyingDNS(t *testing.T) {
	f := newFixture(t)
	operation := f.apply(t, f.plan(t, "retain-plan-001", true), "retain-apply-001")
	confirmed := f.engine.Handle(context.Background(), "confirm", Request{OperationID: operation.Id, RequestID: "retain-confirm-001", Confirm: true})
	if confirmed.Error != "" {
		t.Fatal(confirmed.Error)
	}
	original, err := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if err != nil {
		t.Fatal(err)
	}
	plan := f.plan(t, "retain-plan-002", false)
	if plan.ChangesDns || plan.Config.Dns.ChromeMode != "system" {
		t.Fatal("unchanged DNS silently discarded prior approval or requested new authorization")
	}
	request := applyRequest(plan, "retain-apply-002")
	request.DNSAuthorized = false
	response := f.engine.Handle(context.Background(), "apply", request)
	if response.Error != "" || response.Operation.State != "applied" {
		t.Fatalf("non-DNS region change required DNS privilege: %+v", response)
	}
	current, err := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("non-DNS change rewrote system DNS")
	}
}

func TestDNSOwnerChangeBlocksRollbackBeforeAnyFileWrite(t *testing.T) {
	f := newFixture(t)
	operation := f.apply(t, f.plan(t, "owner-plan-001", true), "owner-apply-001")
	writes := f.backend.writes
	f.backend.manager = "external-owner"
	f.now = f.now.Add(121 * time.Second)
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current := f.operation(t, operation.Id); current.State != "rollback_conflict" || f.backend.writes != writes {
		t.Fatalf("changed resolver owner was overwritten: %v", current)
	}
}

func TestLeaseUsesBootElapsedWhenWallClockMovesBackward(t *testing.T) {
	f := newFixture(t)
	operation := f.apply(t, f.plan(t, "monotonic-plan-001", true), "monotonic-apply-001")
	f.now = f.now.Add(-24 * time.Hour)
	f.backend.elapsed = int64(121 * time.Second)
	f.restart()
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current := f.operation(t, operation.Id); current.State != "rolled_back" {
		t.Fatalf("wall-clock adjustment extended privileged DNS lease: %v", current)
	}
}

func TestUnconfirmedLeaseIsRecoveredImmediatelyAfterBoot(t *testing.T) {
	f := newFixture(t)
	operation := f.apply(t, f.plan(t, "reboot-plan-001", true), "reboot-apply-001")
	f.backend.boot = "different-kernel-boot"
	f.restart()
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current := f.operation(t, operation.Id); current.State != "rolled_back" {
		t.Fatalf("unconfirmed preboot DNS state survived a new boot: %v", current)
	}
}

func TestPackagedLocaleAliasDoesNotRedirectPrivilegedWrites(t *testing.T) {
	canonical := FileState{Kind: "file", Data: []byte("LANG=C.UTF-8\n"), Mode: 0644}
	for _, target := range []string{"../locale.conf", "/etc/locale.conf"} {
		key, state, err := selectLocaleState(FileState{Kind: "symlink", Target: target}, canonical)
		if err != nil || key != "locale-canonical" || !sameFile(state, canonical) {
			t.Fatalf("packaged locale alias lost its effective contents: %q %v", key, err)
		}
	}
	for _, target := range []string{"/etc/shadow", "../../home/user/locale.conf", "../locale.conf/extra"} {
		if _, _, err := selectLocaleState(FileState{Kind: "symlink", Target: target}, canonical); err == nil {
			t.Fatalf("arbitrary privileged locale target accepted: %s", target)
		}
	}
	if _, _, err := selectLocaleState(FileState{Kind: "symlink", Target: "../locale.conf"}, FileState{Kind: "symlink", Target: "/etc/shadow"}); err == nil {
		t.Fatal("second locale symlink accepted")
	}
}

func TestDebianNativeRecordRequiresMatchingRootProvenance(t *testing.T) {
	record := FileState{Kind: "file", Data: []byte("nameserver 127.0.0.1\n")}
	provenance := FileState{Kind: "file", Data: []byte(digest(record.Data)), Mode: 0600}
	if !ownsDebianResolverRecord(record, provenance) {
		t.Fatal("native comment-free record lost authenticated ownership")
	}
	for _, other := range []FileState{{Kind: "absent"}, {Kind: "file", Data: []byte("nameserver 127.0.0.2\n")}, {Kind: "symlink", Target: "/etc/shadow"}} {
		if ownsDebianResolverRecord(other, provenance) {
			t.Fatal("missing, externally modified or redirected record was claimed")
		}
	}
	if ownsDebianResolverRecord(record, FileState{Kind: "absent"}) {
		t.Fatal("unowned reserved record could be overwritten")
	}
}

func TestResolvedReadTrustDoesNotAdmitWritableOrWrongTypeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolver")
	if err := os.WriteFile(path, []byte("nameserver 127.0.0.53\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	uid := uint32(os.Getuid())
	if err := trustedResolvedDescriptor(int(file.Fd()), uid, unix.S_IFREG); err != nil {
		t.Fatal(err)
	}
	if uid != 0 && trustedResolvedDescriptor(int(file.Fd()), uid+1, unix.S_IFREG) == nil {
		t.Fatal("unrelated service account was trusted")
	}
	if trustedResolvedDescriptor(int(file.Fd()), uid, unix.S_IFDIR) == nil {
		t.Fatal("regular file accepted as resolver runtime directory")
	}
	if err := file.Chmod(0666); err != nil {
		t.Fatal(err)
	}
	if trustedResolvedDescriptor(int(file.Fd()), uid, unix.S_IFREG) == nil {
		t.Fatal("world-writable resolver output was trusted")
	}
}
