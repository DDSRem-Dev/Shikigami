//go:build linux

package helper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

// Run is the only production privileged entry point. Both command arguments and
// JSON are closed allowlists. No test backend, alternate root or path is exposed.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_ = EncodeResponse(stdout, Response{Code: "invalid_argument", Error: "exactly one installed helper action is required"})
		return 2
	}
	action := args[0]
	allowed := action == "guard" || action == "prepare-uninstall" || action == "activate"
	for _, candidate := range Actions {
		if action == candidate {
			allowed = true
		}
	}
	if !allowed {
		_ = EncodeResponse(stdout, Response{Code: "invalid_argument", Error: "unsupported installed helper action"})
		return 2
	}
	policy, err := loadPolicy()
	if err == nil {
		err = authorizeInvocation(action, policy)
	}
	if err == nil {
		err = trustedDirectory(StatePath)
	}
	if err == nil {
		info, statErr := os.Stat(StatePath)
		if statErr != nil {
			err = statErr
		} else if info.Mode().Perm() != 0700 {
			err = errors.New("root region state directory must have mode 0700")
		}
	}
	if err != nil {
		_ = EncodeResponse(stdout, errorResponse(err))
		return 1
	}
	engine := NewEngine(NewFileStore(StatePath, true), SystemBackend{}, Options{PlanTTL: time.Duration(policy.PlanTTLSeconds) * time.Second, ConfirmTTL: time.Duration(policy.DNSConfirmSeconds) * time.Second})
	if action == "guard" {
		// The installed systemd service owns this lifetime; no plugin/client
		// process owns its cancellation. At boot it recovers old root leases.
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			tickCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
			err := engine.GuardTick(tickCtx)
			cancel()
			if err != nil {
				fmt.Fprintln(stderr, "region recovery guard:", err)
			}
			select {
			case <-ctx.Done():
				return 0
			case <-ticker.C:
			}
		}
	}
	// Do not let a same-UID caller hold privileged stdin open indefinitely.
	type decoded struct {
		request Request
		err     error
	}
	requests := make(chan decoded, 1)
	go func() { request, err := DecodeRequest(stdin); requests <- decoded{request, err} }()
	var request Request
	select {
	case input := <-requests:
		request, err = input.request, input.err
	case <-time.After(10 * time.Second):
		err = fail("deadline_exceeded", "region helper stdin deadline exceeded")
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil {
		_ = EncodeResponse(stdout, errorResponse(err))
		return 1
	}
	callCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	response := engine.Handle(callCtx, action, request)
	if err := EncodeResponse(stdout, response); err != nil {
		fmt.Fprintln(stderr, "region helper response:", err)
		return 1
	}
	if response.Error != "" {
		return 1
	}
	return 0
}

// InspectReadOnly is used only when the installed authorization is unavailable.
// It does not open root state, acquire privilege, or pretend desired settings
// and successful mutations exist.
func InspectReadOnly(ctx context.Context) (*epb.RegionStatus, error) {
	observation, err := (SystemBackend{}).Inspect(ctx)
	if err != nil {
		return nil, err
	}
	status := &epb.RegionStatus{Actual: observation.Actual, Revision: digest(observation.Fingerprint), DnsManager: observation.Manager, HelperAvailable: false, ObservedAt: time.Now().Unix(), State: "helper_unavailable", Warnings: observation.Warnings}
	status.Warnings = append(status.Warnings, "Administrator-installed immutable region helper authorization is unavailable; planning and applying require installation approval")
	for _, dependency := range observation.Missing {
		status.Warnings = append(status.Warnings, "Missing administrator-installed dependency: "+dependency)
	}
	return status, nil
}
