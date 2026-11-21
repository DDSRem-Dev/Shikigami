//go:build linux

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func readinessSpec(t *testing.T) Spec {
	t.Helper()
	directory, err := os.MkdirTemp("", "browser-ready-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	return Spec{ProfileID: "profile", ProfileRevision: "revision", InstanceID: "instance", Token: "token", Unit: "shiki-browser-test", ConfigPath: filepath.Join(directory, "launch.json"), SocketDir: directory, ProfileDir: directory, ExecutableSHA256: strings.Repeat("a", 64), RegionRevision: "region-revision", Width: 1280, Height: 720}
}

func readinessProperties(spec Spec, state string) map[string]string {
	return map[string]string{"LoadState": "loaded", "ActiveState": state, "Description": "ShikigamiBrowser:" + spec.InstanceID, "Transient": "yes", "ExecStart": "{ path=" + Binary + " ; argv[]=" + Binary + " --config " + spec.ConfigPath + " ; ignore_errors=no ; }"}
}

func readinessServer(t *testing.T, spec Spec, snapshot func() Snapshot) *atomic.Int32 {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(spec.SocketDir, "control.sock"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(spec.SocketDir, "control.sock"), 0600); err != nil {
		listener.Close()
		t.Fatal(err)
	}
	requests := new(atomic.Int32)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/status" || request.Header.Get("Authorization") != "Bearer "+spec.Token {
			writer.WriteHeader(http.StatusForbidden)
			return
		}
		if err := json.NewEncoder(writer).Encode(snapshot()); err != nil {
			t.Errorf("writing runtime status: %v", err)
		}
	})}
	t.Cleanup(func() { server.Close(); listener.Close() })
	go func() { _ = server.Serve(listener) }()
	return requests
}

func TestRuntimeInspectRequiresObservedModeMatch(t *testing.T) {
	for _, test := range []struct {
		name, requested, reported string
		accept                    bool
	}{
		{"legacy", "", "", true},
		{"browse", ModeBrowse, ModeBrowse, true},
		{"diagnostic", ModeDiagnostic, ModeDiagnostic, true},
		{"missing", ModeBrowse, "", false},
		{"different", ModeBrowse, ModeDiagnostic, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := readinessSpec(t)
			spec.Mode = test.requested
			readinessServer(t, spec, func() Snapshot {
				return Snapshot{ProfileID: spec.ProfileID, InstanceID: spec.InstanceID, State: "running", Mode: test.reported}
			})
			got, err := inspectRuntime(t.Context(), spec, func(context.Context, string) (map[string]string, error) {
				return readinessProperties(spec, "active"), nil
			})
			if test.accept {
				if err != nil || got.Mode != spec.BrowserMode() {
					t.Fatalf("mode not retained: %#v %v", got, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "browser mode mismatch") {
				t.Fatalf("mismatched runtime accepted: %#v %v", got, err)
			}
		})
	}
}

func TestRuntimeInspectWaitsForVerifiedStartingUnitWithoutRelaunch(t *testing.T) {
	for _, unitState := range []string{"active", "activating"} {
		for _, endpoint := range []string{"missing", "refused", "starting"} {
			t.Run(unitState+"-"+endpoint, func(t *testing.T) {
				spec := readinessSpec(t)
				var starting atomic.Bool
				starting.Store(endpoint == "starting")
				status := func() Snapshot {
					state := "running"
					if starting.Load() {
						state = "starting"
					}
					return Snapshot{ProfileID: spec.ProfileID, InstanceID: spec.InstanceID, RegionRevision: spec.RegionRevision, State: state}
				}
				var requests *atomic.Int32
				if endpoint == "starting" {
					requests = readinessServer(t, spec, status)
				} else if endpoint == "refused" {
					listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(spec.SocketDir, "control.sock"), Net: "unix"})
					if err != nil {
						t.Fatal(err)
					}
					listener.SetUnlinkOnClose(false)
					if err := os.Chmod(listener.Addr().String(), 0600); err != nil {
						t.Fatal(err)
					}
					listener.Close()
				}
				calls := 0
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				snapshot, err := inspectRuntime(ctx, spec, func(_ context.Context, unit string) (map[string]string, error) {
					calls++
					if unit != spec.Unit {
						t.Fatalf("inspected unrelated unit %q", unit)
					}
					if calls == 3 {
						if endpoint == "starting" {
							starting.Store(false)
						} else {
							if endpoint == "refused" {
								if err := os.Remove(filepath.Join(spec.SocketDir, "control.sock")); err != nil {
									t.Fatal(err)
								}
							}
							requests = readinessServer(t, spec, status)
						}
					}
					return readinessProperties(spec, unitState), nil
				})
				if err != nil || snapshot.State != "running" || snapshot.InstanceID != spec.InstanceID || calls != 3 || requests == nil || requests.Load() == 0 {
					t.Fatalf("reattachment did not await the same unit: %#v err=%v lookups=%d", snapshot, err, calls)
				}
			})
		}
	}
}

func TestRuntimeInspectRechecksUnitIdentityAndExitWhileWaiting(t *testing.T) {
	for _, change := range []string{"description", "executable", "absent", "exited"} {
		t.Run(change, func(t *testing.T) {
			spec := readinessSpec(t)
			calls := 0
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			snapshot, err := inspectRuntime(ctx, spec, func(context.Context, string) (map[string]string, error) {
				calls++
				properties := readinessProperties(spec, "activating")
				if calls == 2 {
					switch change {
					case "description":
						properties["Description"] = "ShikigamiBrowser:replacement"
					case "executable":
						properties["ExecStart"] = "unrelated command"
					case "absent":
						properties["LoadState"] = "not-found"
					case "exited":
						properties["ActiveState"], properties["Result"] = "failed", "exit-code"
					}
				}
				return properties, nil
			})
			if calls != 2 {
				t.Fatalf("did not reverify the waiting unit: %d %v", calls, err)
			}
			if change == "absent" || change == "exited" {
				if err != nil || snapshot.State != "crashed" {
					t.Fatalf("exited runtime was relaunched or not reported: %#v %v", snapshot, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
				t.Fatalf("adopted a changed runtime: %#v %v", snapshot, err)
			}
		})
	}
}

func TestRuntimeInspectDoesNotRetryUntrustedControlEndpoints(t *testing.T) {
	for _, mismatch := range []string{"profile", "instance", "missing-profile", "missing-instance", "token", "ordinary-file"} {
		t.Run(mismatch, func(t *testing.T) {
			spec := readinessSpec(t)
			if mismatch == "ordinary-file" {
				if err := os.WriteFile(filepath.Join(spec.SocketDir, "control.sock"), []byte("not a socket"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				serverSpec := spec
				if mismatch == "token" {
					serverSpec.Token = "different-token"
				}
				readinessServer(t, serverSpec, func() Snapshot {
					snapshot := Snapshot{ProfileID: spec.ProfileID, InstanceID: spec.InstanceID, State: "running"}
					if mismatch == "profile" {
						snapshot.ProfileID = "different-profile"
					} else if mismatch == "instance" {
						snapshot.InstanceID = "different-instance"
					} else if mismatch == "missing-profile" {
						snapshot.ProfileID = ""
					} else if mismatch == "missing-instance" {
						snapshot.InstanceID = ""
					}
					return snapshot
				})
			}
			calls := 0
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := inspectRuntime(ctx, spec, func(context.Context, string) (map[string]string, error) {
				calls++
				return readinessProperties(spec, "active"), nil
			})
			if err == nil || calls != 1 {
				t.Fatalf("retried or accepted an untrusted endpoint: %v lookups=%d", err, calls)
			}
		})
	}
}

func TestRuntimeInspectReadinessWaitHonorsCancellation(t *testing.T) {
	spec := readinessSpec(t)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	calls := 0
	_, err := inspectRuntime(ctx, spec, func(context.Context, string) (map[string]string, error) {
		calls++
		return readinessProperties(spec, "activating"), nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls < 2 || calls > 4 {
		t.Fatalf("readiness wait was not bounded by its caller: %v lookups=%d", err, calls)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	_, err = inspectRuntime(canceled, spec, func(context.Context, string) (map[string]string, error) {
		t.Fatal("queried a unit after cancellation")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}
