package node

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSDKProxyFeatureInitialization(t *testing.T) {
	if mode := os.Getenv("SHIKI_TEST_PROXY_FEATURE_CHILD"); mode != "" {
		err := requireSDKProxyHooks()
		if mode == "disabled" {
			if !errors.Is(err, errSDKProxyInitialization) {
				t.Fatal("disabled proxy feature was accepted", err)
			}
			if !errors.Is(configureSDKNetworking(), errSDKProxyInitialization) {
				t.Fatal("node configuration would bypass disabled proxy feature")
			}
		} else if err != nil {
			t.Fatal("normal SDK proxy hooks missing", err)
		}
		return
	}
	for _, mode := range []string{"enabled", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSDKProxyFeatureInitialization$", "-test.count=1")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "TS_DISABLE_FEATURE=") && !strings.HasPrefix(entry, "SHIKI_TEST_PROXY_FEATURE_CHILD=") {
					command.Env = append(command.Env, entry)
				}
			}
			command.Env = append(command.Env, "SHIKI_TEST_PROXY_FEATURE_CHILD="+mode)
			if mode == "disabled" {
				command.Env = append(command.Env, "TS_DISABLE_FEATURE=useproxy")
			}
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("SDK init subprocess failed: %v\n%s", err, output)
			}
		})
	}
}
