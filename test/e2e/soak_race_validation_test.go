//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The failing test runs in a subprocess: its body accepts the fixture's known
// exit status, so only configureSoakRace's cleanup can reject the race report.
func TestSoakRaceReportRejectsRacingChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "race.go")
	const program = `package main
import ("fmt"; "os")
func main() {
 if len(os.Args) > 1 && os.Args[1] == "clean" { return }
 start, done := make(chan struct{}), make(chan struct{})
 value := 0
 go func() { <-start; for i := 0; i < 100000; i++ { value++ }; close(done) }()
 close(start)
 for i := 0; i < 100000; i++ { value++ }
 <-done
 fmt.Println(value)
}
`
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "fixture")
	if output, err := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, source).CombinedOutput(); err != nil {
		t.Fatalf("build real race fixture: %v\n%s", err, output)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range []string{"sync", "port"} {
		for _, mode := range []string{"clean", "race"} {
			t.Run(plugin+"/"+mode, func(t *testing.T) {
				fixture := t.TempDir()
				bin := filepath.Join(fixture, "bin")
				if err := os.Mkdir(bin, 0700); err != nil {
					t.Fatal(err)
				}
				for _, id := range []string{"sync", "port"} {
					if err := os.WriteFile(filepath.Join(bin, "shiki-plugin-"+id), data, 0700); err != nil {
						t.Fatal(err)
					}
				}
				command := exec.CommandContext(ctx, self, "-test.run=^TestSoakRaceReportFixture$", "-test.v", "-shiki-soak-duration=1m", "-shiki-soak-tcp-clients=1")
				for _, entry := range os.Environ() {
					if !strings.HasPrefix(entry, "SHIKI_SOAK_RACE_FIXTURE=") && !strings.HasPrefix(entry, "SHIKI_SOAK_RACE_MODE=") && !strings.HasPrefix(entry, "SHIKI_SOAK_RACE_PLUGIN=") {
						command.Env = append(command.Env, entry)
					}
				}
				command.Env = append(command.Env, "SHIKI_SOAK_RACE_FIXTURE="+fixture, "SHIKI_SOAK_RACE_MODE="+mode, "SHIKI_SOAK_RACE_PLUGIN="+plugin)
				output, err := command.CombinedOutput()
				if mode == "clean" {
					if err != nil {
						t.Fatalf("clean child rejected: %v\n%s", err, output)
					}
					return
				}
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(output, []byte("shipped child produced a race report")) || !bytes.Contains(output, []byte("WARNING: DATA RACE")) {
					t.Fatalf("real race did not fail the report cleanup: %v\n%s", err, output)
				}
			})
		}
	}
}

func TestSoakRaceReportFixture(t *testing.T) {
	dir := os.Getenv("SHIKI_SOAK_RACE_FIXTURE")
	if dir == "" {
		return
	}
	bin := filepath.Join(dir, "bin")
	configureSoakRace(t, dir, bin)
	plugin := os.Getenv("SHIKI_SOAK_RACE_PLUGIN")
	if plugin != "sync" && plugin != "port" {
		t.Fatal("invalid race fixture plugin")
	}
	command := exec.CommandContext(t.Context(), filepath.Join(bin, "shiki-plugin-"+plugin), os.Getenv("SHIKI_SOAK_RACE_MODE"))
	// Simulate the production host's environment filter: the wrapper must set
	// GORACE itself after filtering, not depend on inherited test options.
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	output, err := command.CombinedOutput()
	if os.Getenv("SHIKI_SOAK_RACE_MODE") == "clean" {
		if err != nil {
			t.Fatalf("clean fixture: %v\n%s", err, output)
		}
		return
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 66 {
		t.Fatalf("expected race runtime exit 66: %v\n%s", err, output)
	}
}
