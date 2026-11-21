//go:build linux

package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
)

// Frozen alpha.8 Spec shape: adding a launch field must not make profiles.json
// unreadable by its DisallowUnknownFields decoder during artifact rollback.
type alpha8RuntimeSpec struct {
	ProfileID        string            `json:"profile_id"`
	ProfileRevision  string            `json:"profile_revision"`
	InstanceID       string            `json:"instance_id"`
	Token            string            `json:"token"`
	Unit             string            `json:"unit"`
	ConfigPath       string            `json:"config_path"`
	SocketDir        string            `json:"socket_dir"`
	ProfileDir       string            `json:"profile_dir"`
	ExecutableSHA256 string            `json:"executable_sha256"`
	Region           *epb.RegionConfig `json:"region,omitempty"`
	RegionRevision   string            `json:"region_revision"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
}
type alpha8Database struct {
	database
	Runtime *alpha8RuntimeSpec `json:"runtime,omitempty"`
}

func TestModePersistenceSurvivesAlpha8DatabaseRewriteAndReupgrade(t *testing.T) {
	for _, mode := range []string{browserruntime.ModeBrowse, browserruntime.ModeDiagnostic, ""} {
		t.Run("mode-"+mode, func(t *testing.T) {
			state := t.TempDir()
			manager := &testManager{}
			worker := newWorker(t, state, "boot", manager)
			putProfile(t, worker, "one", false)
			request := &epb.BrowserActionRequest{ProfileId: "one", RequestId: "original", Confirm: true, Diagnostics: mode != browserruntime.ModeBrowse}
			original, err := worker.StartBrowser(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "" {
				worker.mu.Lock()
				worker.db.Runtime.Mode = ""
				worker.db.Runtime.ConfigPath = filepath.Join(state, "runtime", worker.db.Runtime.InstanceID+".json")
				if err := browserruntime.AtomicJSON(worker.db.Runtime.ConfigPath, worker.db.Runtime, browserruntime.MaxControlBody); err != nil {
					t.Fatal(err)
				}
				worker.mu.Unlock()
			}
			if _, err := worker.StopBrowser(t.Context(), &epb.BrowserActionRequest{ProfileId: "one", RequestId: "stop", Confirm: true}); err != nil {
				t.Fatal(err)
			}
			if err := worker.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(state, "profiles.json")
			var legacy alpha8Database
			if err := browserruntime.ReadJSON(path, &legacy, maxDatabaseBytes); err != nil {
				t.Fatal("alpha.8 strict reader rejected new state", err)
			}
			if legacy.Runtime == nil || legacy.Runtime.InstanceID != original.InstanceId {
				t.Fatal("legacy runtime identity lost")
			}
			if err := browserruntime.AtomicJSON(path, &legacy, maxDatabaseBytes); err != nil {
				t.Fatal(err)
			}
			worker = newWorker(t, state, "boot", manager)
			if worker.db.Runtime.Mode != mode || worker.persisted.Runtime.Mode != mode {
				t.Fatal("reupgrade lost immutable mode", worker.db.Runtime.Mode)
			}
			status, err := worker.GetBrowserStatus(t.Context(), &epb.BrowserScope{ProfileId: "one"})
			expected := mode
			if expected == "" {
				expected = browserruntime.ModeDiagnostic
			}
			if err != nil || status.Mode != expected || status.State != "stopped" {
				t.Fatal(status, err)
			}
			replay, err := worker.StartBrowser(t.Context(), request)
			if err != nil || replay.InstanceId != original.InstanceId || replay.Mode != expected || manager.launches != 1 {
				t.Fatal("reupgrade lost replay or relaunched", replay, err)
			}
		})
	}
}

func TestModeEvidenceMissingDamagedOrMismatchedFailsClosed(t *testing.T) {
	cases := map[string]func(t *testing.T, spec browserruntime.Spec){
		"missing": func(t *testing.T, spec browserruntime.Spec) {
			if err := os.Remove(spec.ConfigPath); err != nil {
				t.Fatal(err)
			}
		},
		"malformed": func(t *testing.T, spec browserruntime.Spec) {
			if err := os.WriteFile(spec.ConfigPath, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"wrong-token": func(t *testing.T, spec browserruntime.Spec) {
			spec.Token = browserruntime.NewID()
			if err := browserruntime.AtomicJSON(spec.ConfigPath, spec, browserruntime.MaxControlBody); err != nil {
				t.Fatal(err)
			}
		},
		"wrong-region": func(t *testing.T, spec browserruntime.Spec) {
			spec.Region = &epb.RegionConfig{Country: "US"}
			if err := browserruntime.AtomicJSON(spec.ConfigPath, spec, browserruntime.MaxControlBody); err != nil {
				t.Fatal(err)
			}
		},
		"missing-mode": func(t *testing.T, spec browserruntime.Spec) {
			spec.Mode = ""
			if err := browserruntime.AtomicJSON(spec.ConfigPath, spec, browserruntime.MaxControlBody); err != nil {
				t.Fatal(err)
			}
		},
		"invalid-mode": func(t *testing.T, spec browserruntime.Spec) {
			spec.Mode = "guessed"
			if err := browserruntime.AtomicJSON(spec.ConfigPath, spec, browserruntime.MaxControlBody); err != nil {
				t.Fatal(err)
			}
		},
		"shared-permissions": func(t *testing.T, spec browserruntime.Spec) {
			if err := os.Chmod(spec.ConfigPath, 0644); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T, spec browserruntime.Spec) {
			target := spec.ConfigPath + ".real"
			if err := os.Rename(spec.ConfigPath, target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, spec.ConfigPath); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			state := t.TempDir()
			manager := &testManager{}
			worker := newWorker(t, state, "boot", manager)
			putProfile(t, worker, "one", false)
			startProfile(t, worker, "one", "start")
			if _, err := worker.StopBrowser(t.Context(), &epb.BrowserActionRequest{ProfileId: "one", RequestId: "stop", Confirm: true}); err != nil {
				t.Fatal(err)
			}
			spec := *worker.db.Runtime
			if err := worker.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			damage(t, spec)
			restored := NewWithManager(manager)
			err := restored.Init(t.Context(), sdk.InitContext{Side: "server", StateDir: state})
			if err == nil {
				restored.Stop(t.Context())
				t.Fatal("unverified evidence guessed as legacy")
			}
			if manager.launches != 1 || manager.stops != 1 {
				t.Fatal("failed init touched runtime")
			}
		})
	}
}

func TestModeSurvivesFailedSaveRollbackAndPublishedSave(t *testing.T) {
	manager := &testManager{}
	worker := newWorker(t, t.TempDir(), "boot", manager)
	putProfile(t, worker, "one", false)
	startProfile(t, worker, "one", "start")
	worker.mu.Lock()
	defer worker.mu.Unlock()
	writer := worker.writeJSON
	failure := errors.New("injected save failure")
	worker.writeJSON = func(string, any, int64) error { return failure }
	worker.db.Runtime.Mode = browserruntime.ModeDiagnostic
	worker.db.Receipts["uncommitted"] = receipt{Method: "start"}
	if err := worker.saveLocked(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if worker.db.Runtime.Mode != browserruntime.ModeBrowse || worker.persisted.Runtime.Mode != browserruntime.ModeBrowse {
		t.Fatal("failed save discarded runtime mode")
	}
	if _, exists := worker.db.Receipts["uncommitted"]; exists {
		t.Fatal("failed receipt retained")
	}
	worker.writeJSON = func(path string, value any, limit int64) error {
		if err := writer(path, value, limit); err != nil {
			return err
		}
		return &browserruntime.PublishedError{Err: failure}
	}
	if err := worker.saveLocked(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	worker.writeJSON = writer
	if worker.db.Runtime.Mode != browserruntime.ModeBrowse || worker.persisted.Runtime.Mode != browserruntime.ModeBrowse {
		t.Fatal("published save discarded runtime mode")
	}
	raw, err := os.ReadFile(filepath.Join(worker.stateDir, "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "browser_mode") {
		t.Fatal("launch mode leaked into legacy database")
	}
	clone := cloneDatabase(worker.db)
	clone.Runtime.Mode = browserruntime.ModeDiagnostic
	if worker.db.Runtime.Mode != browserruntime.ModeBrowse {
		t.Fatal("database clone aliases runtime")
	}
	var status epb.BrowserStatus
	if err := json.Unmarshal(worker.db.Receipts["start"].Result, &status); err != nil || status.Mode != browserruntime.ModeBrowse {
		t.Fatal("receipt lost mode", err)
	}
}

func TestModeEvidenceRejectsEscapingIdentityBeforeReadingLaunch(t *testing.T) {
	for _, field := range []string{"profile", "instance", "path"} {
		t.Run(field, func(t *testing.T) {
			state := t.TempDir()
			spec := browserruntime.Spec{ProfileID: "one", InstanceID: "instance"}
			switch field {
			case "profile":
				spec.ProfileID = "../one"
			case "instance":
				spec.InstanceID = "../../outside"
			}
			spec.ConfigPath = filepath.Join(state, "runtime", spec.InstanceID+".launch.json")
			if field == "path" {
				spec.ConfigPath = filepath.Join(state, "outside.launch.json")
			}
			err := hydrateRuntimeMode(state, &spec)
			if err == nil || strings.Contains(err.Error(), "cannot verify browser launch mode") {
				t.Fatal("unsafe identity/path reached file read", err)
			}
		})
	}
}

func TestLegacyLaunchFilenameCannotCarryNewMode(t *testing.T) {
	state := t.TempDir()
	manager := &testManager{}
	spec, err := manager.Prepare(t.Context(), state, "one", "profile-revision", "instance", nil, "region", 1280, 720, browserruntime.ModeBrowse)
	if err != nil {
		t.Fatal(err)
	}
	spec.ConfigPath = filepath.Join(state, "runtime", "instance.json")
	if err := browserruntime.AtomicJSON(spec.ConfigPath, spec, browserruntime.MaxControlBody); err != nil {
		t.Fatal(err)
	}
	disk := spec
	disk.Mode = ""
	if err := hydrateRuntimeMode(state, &disk); err == nil || !strings.Contains(err.Error(), "generation") {
		t.Fatal("new mode accepted as legacy record", err)
	}
}
