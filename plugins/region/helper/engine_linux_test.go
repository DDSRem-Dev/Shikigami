//go:build linux

package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

// diskBackend exercises the actual persistent transaction engine using only
// temporary files. Its dependency-failure switches never run privileged tools,
// change the test host, or send a network probe.
type diskBackend struct {
	directory       string
	manager         string
	boot            string
	elapsed         int64
	writes          int
	checkErr        error
	prepareErr      error
	writeFault      func(string, FileState) error
	reloadFault      func(context.Context, Transition) error
	crashAfterWrite bool
}

func newDiskBackend(t *testing.T) *diskBackend {
	t.Helper()
	backend := &diskBackend{directory: t.TempDir(), manager: "debian-static", boot: "test-boot-1"}
	initial := &epb.RegionConfig{Country: "US", Timezone: "America/New_York", Locale: "en_US.UTF-8", Languages: []string{"en_US", "en"}, Formats: map[string]string{}, Dns: &epb.DNSConfig{ChromeMode: "unchanged"}}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string][]byte{"environment": data, "resolver": []byte("nameserver 192.0.2.1\n"), "owner": []byte("static-owner"), "resolvconf-head": []byte("# head\n"), "resolvconf-tail": []byte("# tail\n"), "resolvconf-order": []byte("shikigami.region\neth*\n")} {
		if err := os.WriteFile(filepath.Join(backend.directory, key), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return backend
}
func (b *diskBackend) path(key string) (string, error) {
	switch key {
	case "environment", "resolver", "owner", "resolvconf-head", "resolvconf-tail", "resolvconf-order", "resolvconf-record", "resolvconf-owner":
		return filepath.Join(b.directory, key), nil
	}
	return "", errors.New("test backend rejects unknown key")
}
func (b *diskBackend) Read(_ context.Context, key string) (FileState, error) {
	path, err := b.path(key)
	if err != nil {
		return FileState{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return FileState{Kind: "absent"}, nil
	}
	if err != nil {
		return FileState{}, err
	}
	return FileState{Kind: "file", Data: data, Mode: 0600}, nil
}
func (b *diskBackend) Write(_ context.Context, key string, state FileState) error {
	if b.writeFault != nil {
		if err := b.writeFault(key, state); err != nil {
			return err
		}
	}
	path, err := b.path(key)
	if err != nil {
		return err
	}
	b.writes++
	if state.Kind == "absent" {
		err = os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	} else {
		err = os.WriteFile(path, state.Data, os.FileMode(state.Mode))
	}
	if err == nil && b.crashAfterWrite {
		b.crashAfterWrite = false
		panic("simulated helper process death after filesystem write before checkpoint")
	}
	return err
}
func (b *diskBackend) BootIdentity(context.Context) (string, error) { return b.boot, nil }
func (b *diskBackend) BootElapsed(context.Context) (int64, error)   { return b.elapsed, nil }
func (b *diskBackend) Inspect(ctx context.Context) (Observation, error) {
	state, err := b.Read(ctx, "environment")
	if err != nil {
		return Observation{}, err
	}
	var config epb.RegionConfig
	if err := json.Unmarshal(state.Data, &config); err != nil {
		return Observation{}, err
	}
	files := make(map[string]FileState)
	for _, key := range []string{"environment", "resolver", "owner", "resolvconf-head", "resolvconf-tail", "resolvconf-order", "resolvconf-record"} {
		current, err := b.Read(ctx, key)
		if err != nil {
			return Observation{}, err
		}
		files[key] = current
	}
	config.Dns = &epb.DNSConfig{Servers: nameservers(files["resolver"].Data), ChromeMode: "unchanged"}
	if b.manager == "openresolv" || b.manager == "resolvconf" {
		config.Dns.Servers = nameservers(files["resolvconf-record"].Data)
	}
	return Observation{Actual: &config, Manager: b.manager, Fingerprint: digest(files)}, nil
}
func (b *diskBackend) Prepare(ctx context.Context, config *epb.RegionConfig) (Transition, error) {
	if b.prepareErr != nil {
		return Transition{}, b.prepareErr
	}
	transition := Transition{Manager: b.manager, DNS: config.Dns.ManageSystem, Ownership: make(map[string]FileState)}
	data, err := json.Marshal(config)
	if err != nil {
		return transition, err
	}
	before, err := b.Read(ctx, "environment")
	if err != nil {
		return transition, err
	}
	transition.Entries = append(transition.Entries, Mutation{Key: "environment", Before: before, After: FileState{Kind: "file", Data: data, Mode: 0600}})
	if config.Dns.ManageSystem {
		owner, err := b.Read(ctx, "owner")
		if err != nil {
			return transition, err
		}
		transition.Ownership["owner"] = owner
		var dns strings.Builder
		for _, server := range config.Dns.Servers {
			dns.WriteString("nameserver " + server + "\n")
		}
		key := "resolver"
		if b.manager == "openresolv" || b.manager == "resolvconf" {
			key = "resolvconf-record"
		}
		old, err := b.Read(ctx, key)
		if err != nil {
			return transition, err
		}
		transition.Entries = append(transition.Entries, Mutation{Key: key, Before: old, After: FileState{Kind: "file", Data: []byte(dns.String()), Mode: 0600}})
	}
	return transition, nil
}
func (b *diskBackend) Reload(ctx context.Context, transition Transition) error {
	if b.reloadFault != nil {
		return b.reloadFault(ctx, transition)
	}
	return nil
}
func (b *diskBackend) CheckDNS(context.Context, *epb.RegionConfig) ([]string, error) {
	return nil, b.checkErr
}

type fixture struct {
	engine  *Engine
	backend *diskBackend
	state   string
	now     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fixture := &fixture{backend: newDiskBackend(t), state: t.TempDir(), now: time.Unix(1800000000, 0)}
	fixture.restart()
	if err := fixture.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	return fixture
}
func (f *fixture) restart() {
	f.engine = NewEngine(NewFileStore(f.state, false), f.backend, Options{Now: func() time.Time { return f.now }, PlanTTL: 5 * time.Minute, ConfirmTTL: 2 * time.Minute})
}
func (f *fixture) inspect(t *testing.T) *epb.RegionStatus {
	t.Helper()
	response := f.engine.Handle(context.Background(), "inspect", Request{})
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	return response.Status
}
func (f *fixture) plan(t *testing.T, id string, dns bool) *epb.RegionPlan {
	t.Helper()
	config := &epb.RegionConfig{Country: "JP"}
	if dns {
		config.Dns = &epb.DNSConfig{ManageSystem: true, Servers: []string{"1.1.1.1", "9.9.9.9"}, CheckDomains: []string{"example.com"}, ChromeMode: "system"}
	}
	return f.planConfig(t, id, config)
}
func (f *fixture) planConfig(t *testing.T, id string, config *epb.RegionConfig) *epb.RegionPlan {
	t.Helper()
	response := f.engine.Handle(context.Background(), "plan", Request{RequestID: id, ExpectedRevision: f.inspect(t).Revision, Config: config})
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	return response.Plan
}
func applyRequest(plan *epb.RegionPlan, id string) Request {
	return Request{PlanID: plan.Id, ExpectedRevision: plan.Revision, RequestID: id, Confirm: true, DNSAuthorized: plan.ChangesDns}
}
func (f *fixture) apply(t *testing.T, plan *epb.RegionPlan, id string) *epb.RegionOperation {
	t.Helper()
	response := f.engine.Handle(context.Background(), "apply", applyRequest(plan, id))
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	return response.Operation
}
func (f *fixture) operation(t *testing.T, id string) *epb.RegionOperation {
	t.Helper()
	response := f.engine.Handle(context.Background(), "operation", Request{OperationID: id})
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	return response.Operation
}

func TestPlanRevisionDeduplicationAndExpiration(t *testing.T) {
	f := newFixture(t)
	request := Request{Config: &epb.RegionConfig{Country: "JP"}, RequestID: "plan-request-001", ExpectedRevision: f.inspect(t).Revision}
	first := f.engine.Handle(context.Background(), "plan", request)
	second := f.engine.Handle(context.Background(), "plan", request)
	if first.Error != "" || second.Error != "" || first.Plan.Id != second.Plan.Id {
		t.Fatalf("idempotent plan failed: %+v %+v", first, second)
	}
	request.Config = &epb.RegionConfig{Country: "JP", Timezone: "Asia/Osaka"}
	conflict := f.engine.Handle(context.Background(), "plan", request)
	if conflict.Code != "already_exists" {
		t.Fatalf("request id collision: %+v", conflict)
	}
	stale := f.engine.Handle(context.Background(), "plan", Request{Config: &epb.RegionConfig{Country: "JP"}, RequestID: "stale-request-001", ExpectedRevision: "old"})
	if stale.Code != "aborted" {
		t.Fatalf("stale plan: %+v", stale)
	}
	f.now = f.now.Add(6 * time.Minute)
	expired := f.engine.Handle(context.Background(), "apply", applyRequest(first.Plan, "expired-apply-001"))
	if expired.Code != "failed_precondition" || f.backend.writes != 0 {
		t.Fatalf("expired plan mutated host: %+v", expired)
	}
}

func TestApplyConfirmationAuthorizationAndDependencies(t *testing.T) {
	f := newFixture(t)
	plan := f.plan(t, "dns-plan-001", true)
	request := applyRequest(plan, "dns-apply-001")
	request.Confirm = false
	if response := f.engine.Handle(context.Background(), "apply", request); response.Code != "failed_precondition" {
		t.Fatalf("missing confirmation: %+v", response)
	}
	request.Confirm = true
	request.DNSAuthorized = false
	if response := f.engine.Handle(context.Background(), "apply", request); response.Code != "permission_denied" {
		t.Fatalf("missing DNS grant: %+v", response)
	}
	f.backend.prepareErr = fail("failed_precondition", "ja_JP.UTF-8 is not generated; administrator approval is required")
	request.DNSAuthorized = true
	if response := f.engine.Handle(context.Background(), "apply", request); response.Code != "failed_precondition" || f.backend.writes != 0 {
		t.Fatalf("missing dependency mutated host: %+v", response)
	}
	blocked := f.plan(t, "blocked-plan-001", false)
	if !strings.Contains(strings.Join(blocked.Warnings, " "), "Application blocked") {
		t.Fatal("dependency blocker absent from plan")
	}
}

func TestApplyIsDurableIdempotentAndRollbackRestoresSnapshot(t *testing.T) {
	f := newFixture(t)
	before, err := os.ReadFile(filepath.Join(f.backend.directory, "environment"))
	if err != nil {
		t.Fatal(err)
	}
	plan := f.plan(t, "local-plan-001", false)
	applied := f.apply(t, plan, "local-apply-001")
	if applied.State != "applied" || !applied.RestartRequired || applied.Revision == plan.Revision {
		t.Fatalf("bad application: %v", applied)
	}
	writes := f.backend.writes
	f.restart()
	repeat := f.apply(t, plan, "local-apply-001")
	if repeat.Id != applied.Id || f.backend.writes != writes {
		t.Fatal("restarted helper repeated mutation")
	}
	status := f.inspect(t)
	if status.Desired.Country != "JP" || status.Actual.Timezone != "Asia/Tokyo" {
		t.Fatalf("actual/desired not observed: %v", status)
	}
	rollback := f.engine.Handle(context.Background(), "rollback", Request{OperationID: applied.Id, RequestID: "local-rollback-001", Confirm: true})
	if rollback.Error != "" || rollback.Operation.State != "rolled_back" {
		t.Fatalf("rollback: %+v", rollback)
	}
	after, err := os.ReadFile(filepath.Join(f.backend.directory, "environment"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("root snapshot was not restored exactly")
	}
	if info, err := os.Stat(filepath.Join(f.state, "journal.json")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("snapshot journal is not private")
	}
}

func TestIndependentGuardRestoresDNSAfterPluginDisappears(t *testing.T) {
	f := newFixture(t)
	original, err := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if err != nil {
		t.Fatal(err)
	}
	operation := f.apply(t, f.plan(t, "lease-plan-001", true), "lease-apply-001")
	if operation.State != "awaiting_confirmation" || operation.ConfirmBefore == 0 {
		t.Fatalf("no recovery lease: %v", operation)
	}
	status := f.inspect(t)
	if status.PendingOperationId != operation.Id || status.ConfirmBefore != operation.ConfirmBefore {
		t.Fatal("lost response cannot discover pending lease")
	}
	f.now = f.now.Add(121 * time.Second)
	f.restart() // a new helper/guard instance, independent of the old plugin
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recovered := f.operation(t, operation.Id); recovered.State != "rolled_back" || !strings.Contains(recovered.Reason, "lease expired") {
		t.Fatalf("guard result: %v", recovered)
	}
	current, err := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("DNS did not restore after plugin loss")
	}
}

func TestConfirmStopsLeaseButDoesNotRestartApplications(t *testing.T) {
	f := newFixture(t)
	operation := f.apply(t, f.plan(t, "confirm-plan-001", true), "confirm-apply-001")
	request := Request{OperationID: operation.Id, RequestID: "confirm-request-001", Confirm: true}
	confirmed := f.engine.Handle(context.Background(), "confirm", request)
	if confirmed.Error != "" || confirmed.Operation.State != "confirmed" || !confirmed.Operation.RestartRequired {
		t.Fatalf("confirmation: %+v", confirmed)
	}
	f.now = f.now.Add(time.Hour)
	f.restart()
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current := f.operation(t, operation.Id); current.State != "confirmed" {
		t.Fatalf("confirmed DNS reverted: %v", current)
	}
	if repeated := f.engine.Handle(context.Background(), "confirm", request); repeated.Error != "" {
		t.Fatal(repeated.Error)
	}
}

func TestExternalModificationPreventsEveryRollbackWrite(t *testing.T) {
	f := newFixture(t)
	operation := f.apply(t, f.plan(t, "conflict-plan-001", true), "conflict-apply-001")
	external := []byte("nameserver 203.0.113.9\n# administrator edit\n")
	if err := os.WriteFile(filepath.Join(f.backend.directory, "resolver"), external, 0600); err != nil {
		t.Fatal(err)
	}
	writes := f.backend.writes
	f.now = f.now.Add(121 * time.Second)
	f.restart()
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recovered := f.operation(t, operation.Id); recovered.State != "rollback_conflict" {
		t.Fatalf("external edit was not reported: %v", recovered)
	}
	if f.backend.writes != writes {
		t.Fatal("rollback partially overwrote host despite a conflict")
	}
	current, _ := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if !bytes.Equal(current, external) {
		t.Fatal("administrator edit was overwritten")
	}
	if status := f.inspect(t); status.State != "rollback_conflict" {
		t.Fatal("conflict was hidden after lease cleared")
	}
}

func TestDNSCheckFailureRestoresAndRecoveryFailureRetries(t *testing.T) {
	f := newFixture(t)
	f.backend.checkErr = errors.New("controlled DNS check failure")
	operation := f.apply(t, f.plan(t, "checkfail-plan-001", true), "checkfail-apply-001")
	if operation.State != "rolled_back" || !strings.Contains(operation.Reason, "controlled DNS check failure") {
		t.Fatalf("failed check reported success: %v", operation)
	}
	f.backend.checkErr = nil
	operation = f.apply(t, f.plan(t, "retry-plan-001", true), "retry-apply-001")
	f.backend.writeFault = func(key string, state FileState) error {
		if key == "resolver" && bytes.Contains(state.Data, []byte("192.0.2.1")) {
			return errors.New("controlled restore I/O failure")
		}
		return nil
	}
	f.now = f.now.Add(121 * time.Second)
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current := f.operation(t, operation.Id); current.State != "rollback_failed" {
		t.Fatalf("restore failure hidden: %v", current)
	}
	f.backend.writeFault = nil
	f.restart()
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current := f.operation(t, operation.Id); current.State != "rolled_back" {
		t.Fatalf("recovery did not retry: %v", current)
	}
}

func TestCrashGapBeforeCheckpointIsRecoverable(t *testing.T) {
	f := newFixture(t)
	original, _ := os.ReadFile(filepath.Join(f.backend.directory, "environment"))
	plan := f.plan(t, "crash-plan-001", true)
	f.backend.crashAfterWrite = true
	func() {
		defer func() {
			if recover() == nil {
				t.Error("crash seam was not reached")
			}
		}()
		f.engine.Handle(context.Background(), "apply", applyRequest(plan, "crash-apply-001"))
	}()
	f.now = f.now.Add(121 * time.Second)
	f.restart()
	if err := f.engine.GuardTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(filepath.Join(f.backend.directory, "environment"))
	if !bytes.Equal(current, original) {
		t.Fatal("write-before-checkpoint crash lost original root snapshot")
	}
}

func TestTwoWritersRejectStaleRevision(t *testing.T) {
	f := newFixture(t)
	first := f.plan(t, "race-plan-001", false)
	second := f.plan(t, "race-plan-002", false)
	engines := []*Engine{f.engine, NewEngine(NewFileStore(f.state, false), f.backend, Options{Now: func() time.Time { return f.now }})}
	plans := []*epb.RegionPlan{first, second}
	results := make(chan Response, 2)
	for index := range engines {
		go func(index int) {
			results <- engines[index].Handle(context.Background(), "apply", applyRequest(plans[index], fmt.Sprintf("race-apply-%03d", index)))
		}(index)
	}
	applied, aborted := 0, 0
	for range 2 {
		response := <-results
		if response.Code == "aborted" {
			aborted++
		} else if response.Error == "" && response.Operation.State == "applied" {
			applied++
		} else {
			t.Fatalf("unexpected race result: %+v", response)
		}
	}
	if applied != 1 || aborted != 1 {
		t.Fatalf("writers: applied=%d aborted=%d", applied, aborted)
	}
}

func TestQuiesceFencesLoadedCallersAndActivateReopens(t *testing.T) {
	f := newFixture(t)
	plan := f.plan(t, "quiesce-plan-001", false)
	response := f.engine.Handle(context.Background(), "prepare-uninstall", Request{})
	if response.Error != "" || response.Status.State != "disabled" || response.Status.PendingOperationId != "" {
		t.Fatalf("uninstall proof: %+v", response)
	}
	f.restart() // models another invocation that already loaded root policy
	if response := f.engine.Handle(context.Background(), "apply", applyRequest(plan, "quiesced-apply-001")); response.Code != "permission_denied" || f.backend.writes != 0 {
		t.Fatalf("quiesce allowed a late write: %+v", response)
	}
	if response := f.engine.Handle(context.Background(), "activate", Request{}); response.Error != "" || response.Status.State != "ready" {
		t.Fatalf("activate: %+v", response)
	}
	if operation := f.apply(t, plan, "activated-apply-001"); operation.State != "applied" {
		t.Fatal("activation did not reopen transactions")
	}
}

func TestUninstallCancelsPendingAndPreservesConflict(t *testing.T) {
	f := newFixture(t)
	operation := f.apply(t, f.plan(t, "uninstall-plan-001", true), "uninstall-apply-001")
	response := f.engine.Handle(context.Background(), "prepare-uninstall", Request{})
	if response.Error != "" || response.Status.State != "disabled" || response.Status.PendingOperationId != "" || f.operation(t, operation.Id).State != "rolled_back" {
		t.Fatalf("pending uninstall cancellation: %+v", response)
	}
	f.engine.Handle(context.Background(), "activate", Request{})
	original, err := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if err != nil {
		t.Fatal(err)
	}
	operation = f.apply(t, f.plan(t, "uninstall-plan-002", true), "uninstall-apply-002")
	managed, err := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.backend.directory, "resolver"), []byte("nameserver 203.0.113.8\n"), 0600); err != nil {
		t.Fatal(err)
	}
	response = f.engine.Handle(context.Background(), "prepare-uninstall", Request{})
	if response.Error != "" || response.Status.State != "rollback_conflict" {
		t.Fatalf("unsafe uninstall was not refused: %+v", response)
	}
	if err := os.WriteFile(filepath.Join(f.backend.directory, "resolver"), managed, 0600); err != nil {
		t.Fatal(err)
	}
	f.restart()
	response = f.engine.Handle(context.Background(), "prepare-uninstall", Request{})
	if response.Error != "" || response.Status.State != "disabled" || response.Status.PendingOperationId != "" || f.operation(t, operation.Id).State != "rolled_back" {
		t.Fatalf("administrator-resolved conflict prevented safe uninstall: %+v", response)
	}
	restored, err := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if err != nil || !bytes.Equal(restored, original) {
		t.Fatalf("resolved conflict did not restore the original resolver: %q, %v", restored, err)
	}
}

func TestConfirmedResolvconfSurvivesBootWithoutOverwritingOwner(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			f := newFixture(t)
			f.backend.manager = "openresolv"
			operation := f.apply(t, f.plan(t, "boot-plan-001", true), "boot-apply-001")
			response := f.engine.Handle(context.Background(), "confirm", Request{OperationID: operation.Id, RequestID: "boot-confirm-001", Confirm: true})
			if response.Error != "" {
				t.Fatal(response.Error)
			}
			if err := os.Remove(filepath.Join(f.backend.directory, "resolvconf-record")); err != nil {
				t.Fatal(err)
			}
			f.backend.boot = "test-boot-2"
			if conflict {
				if err := os.WriteFile(filepath.Join(f.backend.directory, "resolvconf-head"), []byte("nameserver 203.0.113.7\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			f.restart()
			if err := f.engine.GuardTick(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(filepath.Join(f.backend.directory, "resolvconf-record"))
			if conflict {
				if !errors.Is(err, os.ErrNotExist) || f.inspect(t).State != "dns_recovery_conflict" {
					t.Fatal("boot recovery overwrote external owner state")
				}
			} else if err != nil {
				t.Fatal("confirmed resolver record was not restored after boot")
			}
		})
	}
}

func TestFileStoreLocksAcrossInstancesAndRejectsCorruption(t *testing.T) {
	directory := t.TempDir()
	first := NewFileStore(directory, false)
	second := NewFileStore(directory, false)
	held := make(chan struct{})
	release := make(chan struct{})
	var running sync.WaitGroup
	running.Add(1)
	go func() {
		defer running.Done()
		_ = first.WithLock(context.Background(), func(*Journal, func() error) error { close(held); <-release; return nil })
	}()
	<-held
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	err := second.WithLock(ctx, func(*Journal, func() error) error { return errors.New("lock admitted a second writer") })
	cancel()
	close(release)
	running.Wait()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cross-instance lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "journal.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := first.WithLock(context.Background(), func(*Journal, func() error) error { t.Error("corrupt journal reached mutation callback"); return nil }); err == nil {
		t.Fatal("corrupt snapshots were ignored")
	}
}

func TestLocaleRenderingIsStableAndDoesNotExecuteAssignments(t *testing.T) {
	config, err := NormalizeConfig(&epb.RegionConfig{Country: "JP", Formats: map[string]string{"LC_TIME": "en_US.UTF-8"}})
	if err != nil {
		t.Fatal(err)
	}
	before := []byte("# admin comment\nLC_ALL=\"C\"\nLANG=\"C\"\nUNRELATED=\"keep\"\n")
	first := localeFile(before, config)
	second := localeFile(first, config)
	if !bytes.Equal(first, second) || bytes.Contains(first, []byte("LC_ALL=")) || !bytes.Contains(first, []byte("UNRELATED=\"keep\"")) {
		t.Fatalf("unstable locale output: %s", second)
	}
	values := parseAssignments("LANG=$(touch /must-not-execute)\nSAFE=\"literal\"\n")
	if values["LANG"] != "" || values["SAFE"] != "literal" {
		t.Fatal("shell syntax was evaluated or trusted as literal locale evidence")
	}
}

func TestEmptyOwnershipCheckpointSurvivesJournalReload(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.backend.directory, "owner"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if err != nil {
		t.Fatal(err)
	}
	operation := f.apply(t, f.plan(t, "empty-owner-plan", true), "empty-owner-apply")
	f.restart()
	result := f.engine.Handle(context.Background(), "rollback", Request{OperationID: operation.Id, RequestID: "empty-owner-rollback", Confirm: true})
	if result.Error != "" || result.Operation.State != "rolled_back" {
		t.Fatalf("empty checkpoint became a false external conflict after persistence: %+v", result)
	}
	restored, err := os.ReadFile(filepath.Join(f.backend.directory, "resolver"))
	if err != nil || !bytes.Equal(restored, original) {
		t.Fatalf("DNS was not restored: %q %v", restored, err)
	}
}

func TestRollbackRejectsABAOperationAfterRestart(t *testing.T) {
	f := newFixture(t)
	original, err := f.backend.Read(context.Background(), "environment")
	if err != nil {
		t.Fatal(err)
	}
	first := f.apply(t, f.plan(t, "aba-plan-first", false), "aba-apply-first")
	us := &epb.RegionConfig{Country: "US", Timezone: "America/New_York", Locale: "en_US.UTF-8", Languages: []string{"en_US", "en"}}
	middle := f.apply(t, f.planConfig(t, "aba-plan-middle", us), "aba-apply-middle")
	latest := f.apply(t, f.plan(t, "aba-plan-latest", false), "aba-apply-latest")
	f.restart()
	before := f.inspect(t)
	writes := f.backend.writes
	stale := f.engine.Handle(context.Background(), "rollback", Request{OperationID: first.Id, RequestID: "aba-stale-rollback", Confirm: true})
	if stale.Code != "aborted" || f.backend.writes != writes || f.inspect(t).Revision != before.Revision || f.operation(t, latest.Id).State != "applied" {
		t.Fatalf("old JP generation overwrote the newer JP transaction: %+v", stale)
	}
	// Ownership is a persisted stack: undoing the latest operation restores
	// its predecessor's exact snapshot and right to roll back, not a guess
	// based on configuration equality.
	for index, operation := range []*epb.RegionOperation{latest, middle, first} {
		f.restart()
		response := f.engine.Handle(context.Background(), "rollback", Request{OperationID: operation.Id, RequestID: fmt.Sprintf("aba-current-rollback-%d", index), Confirm: true})
		if response.Error != "" || response.Operation.State != "rolled_back" {
			t.Fatalf("current owning transaction could not restore its predecessor: %+v", response)
		}
	}
	restored, err := f.backend.Read(context.Background(), "environment")
	if err != nil || !sameFile(restored, original) {
		t.Fatalf("ownership-aware rollback lost the original snapshot: %+v, %v", restored, err)
	}
}

func TestAdministratorRecoversPartialConflictBeforeDesiredPublication(t *testing.T) {
	f := newFixture(t)
	us := &epb.RegionConfig{Country: "US", Timezone: "America/New_York", Locale: "en_US.UTF-8", Languages: []string{"en_US", "en"}}
	previous := f.apply(t, f.planConfig(t, "partial-previous-plan", us), "partial-previous-apply")
	beforeEnvironment, err := f.backend.Read(context.Background(), "environment")
	if err != nil {
		t.Fatal(err)
	}
	beforeResolver, err := f.backend.Read(context.Background(), "resolver")
	if err != nil {
		t.Fatal(err)
	}
	beforeOwner, err := f.backend.Read(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	plan := f.plan(t, "partial-conflict-plan", true)
	externalResolver := []byte("nameserver 203.0.113.8\n# unresolved administrator edit\n")
	f.backend.writeFault = func(key string, state FileState) error {
		if key == "resolver" && bytes.Contains(state.Data, []byte("1.1.1.1")) {
			if err := os.WriteFile(filepath.Join(f.backend.directory, key), externalResolver, 0600); err != nil {
				return err
			}
			return errors.New("controlled partial DNS application failure")
		}
		return nil
	}
	partial := f.apply(t, plan, "partial-conflict-apply")
	f.backend.writeFault = nil
	if partial.State != "rollback_conflict" {
		t.Fatalf("partial application did not preserve its conflict: %v", partial)
	}
	f.restart()
	status := f.inspect(t)
	if status.Desired.Country != "US" || status.Actual.Country != "JP" || status.PendingOperationId != "" {
		t.Fatalf("fixture did not stop before desired publication: %v", status)
	}
	response := f.engine.Handle(context.Background(), "rollback", Request{OperationID: partial.Id, RequestID: "partial-user-recovery", Confirm: true})
	if response.Code != "aborted" {
		t.Fatalf("ordinary caller recovered an administrator-only conflict: %+v", response)
	}
	blockedPlan := f.plan(t, "partial-blocked-plan", false)
	writes := f.backend.writes
	response = f.engine.Handle(context.Background(), "apply", applyRequest(blockedPlan, "partial-blocked-apply"))
	if response.Code != "failed_precondition" || f.backend.writes != writes {
		t.Fatalf("new application discarded unresolved recovery ownership: %+v", response)
	}
	if err := os.WriteFile(filepath.Join(f.backend.directory, "owner"), []byte("another unresolved owner checkpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, resolveResolver := range []bool{false, true} {
		if resolveResolver {
			if err := os.WriteFile(filepath.Join(f.backend.directory, "resolver"), beforeResolver.Data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		f.restart()
		response = f.engine.Handle(context.Background(), "prepare-uninstall", Request{})
		if response.Error != "" || response.Status.State != "rollback_conflict" || f.backend.writes != writes {
			t.Fatalf("administrator recovery overwrote a still-conflicting target: %+v", response)
		}
	}
	if err := os.WriteFile(filepath.Join(f.backend.directory, "owner"), beforeOwner.Data, 0600); err != nil {
		t.Fatal(err)
	}
	f.restart()
	response = f.engine.Handle(context.Background(), "prepare-uninstall", Request{})
	if response.Error != "" || response.Status.State != "disabled" || response.Status.PendingOperationId != "" || response.Status.Desired.Country != "US" || f.operation(t, partial.Id).State != "rolled_back" {
		t.Fatalf("resolved partial transaction could not be recovered by root: %+v", response)
	}
	for key, expected := range map[string]FileState{"environment": beforeEnvironment, "resolver": beforeResolver, "owner": beforeOwner} {
		actual, err := f.backend.Read(context.Background(), key)
		if err != nil || !sameFile(actual, expected) {
			t.Fatalf("partial recovery did not restore %s exactly: %+v, %v", key, actual, err)
		}
	}
	if err := NewFileStore(f.state, false).WithLock(context.Background(), func(j *Journal, _ func() error) error {
		if j.Owner != previous.Id || j.RecoveryProblem != "" {
			t.Fatalf("partial recovery did not restore prior transaction ownership: %+v", j)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestJournalRefusesMissingOrInconsistentOwnership(t *testing.T) {
	cases := map[string]func(*Journal){
		"legacy version": func(j *Journal) { j.Version = 1 },
		"unversioned state": func(j *Journal) { j.Version = 0 },
		"missing owner": func(j *Journal) { j.Owner = "" },
		"unknown owner": func(j *Journal) { j.Owner = "operation-unknown" },
		"self predecessor": func(j *Journal) { j.Operations[j.Owner].BeforeOwner = j.Owner },
		"unowned desired snapshot": func(j *Journal) { j.Operations[j.Owner].BeforeDesired = cloneConfig(j.Desired) },
		"mismatched publication": func(j *Journal) { j.Desired = nil },
		"mismatched pending": func(j *Journal) { j.Pending = "operation-unknown" },
		"mismatched recovery": func(j *Journal) { j.RecoveryProblem = "operation-unknown" },
		"missing recovery lease": func(j *Journal) { j.Operations[j.Owner].Operation.State = "applying" },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.apply(t, f.plan(t, "schema-owner-plan", false), "schema-owner-apply")
			if err := NewFileStore(f.state, false).WithLock(context.Background(), func(j *Journal, save func() error) error {
				corrupt(j)
				return save()
			}); err != nil {
				t.Fatal(err)
			}
			writes := f.backend.writes
			f.restart()
			response := f.engine.Handle(context.Background(), "prepare-uninstall", Request{})
			if response.Code != "failed_precondition" || f.backend.writes != writes {
				t.Fatalf("inconsistent ownership was guessed or mutated: %+v", response)
			}
		})
	}
}

func TestJournalRefusesCyclicOwnershipBeforeMutation(t *testing.T) {
	f := newFixture(t)
	first := f.apply(t, f.plan(t, "cycle-first-plan", false), "cycle-first-apply")
	latest := f.apply(t, f.plan(t, "cycle-latest-plan", false), "cycle-latest-apply")
	if err := NewFileStore(f.state, false).WithLock(context.Background(), func(j *Journal, save func() error) error {
		j.Operations[first.Id].BeforeOwner = latest.Id
		j.Operations[first.Id].BeforeDesired = cloneConfig(j.Operations[latest.Id].Config)
		return save()
	}); err != nil {
		t.Fatal(err)
	}
	writes := f.backend.writes
	f.restart()
	response := f.engine.Handle(context.Background(), "rollback", Request{OperationID: latest.Id, RequestID: "cycle-owner-rollback", Confirm: true})
	if response.Code != "failed_precondition" || f.backend.writes != writes {
		t.Fatalf("cyclic ownership reached privileged rollback: %+v", response)
	}
}
