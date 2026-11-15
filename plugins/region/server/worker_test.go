package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"github.com/DDSRem-Dev/Shikigami/plugins/region/helper"
)

type recordingHelper struct {
	call func(context.Context, string, helper.Request) (helper.Response, error)
}

func (h recordingHelper) Call(ctx context.Context, action string, request helper.Request) (helper.Response, error) {
	return h.call(ctx, action, request)
}
func runningWorker(t *testing.T, client Helper) *Worker {
	t.Helper()
	worker := NewWithHelper(client)
	worker.initialized = true
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := worker.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return worker
}

func TestRegionMethodsUseOnlyTypedHelperRequests(t *testing.T) {
	var mu sync.Mutex
	var actions []string
	worker := runningWorker(t, recordingHelper{call: func(_ context.Context, action string, request helper.Request) (helper.Response, error) {
		mu.Lock()
		actions = append(actions, action)
		mu.Unlock()
		switch action {
		case "inspect":
			return helper.Response{Status: &epb.RegionStatus{HelperAvailable: true, State: "ready", Revision: "observed-revision"}}, nil
		case "plan":
			if request.RequestID != "request-plan-01" || request.ExpectedRevision != "observed-revision" || request.Config.Country != "JP" {
				t.Error("typed plan fields were lost")
			}
			return helper.Response{Plan: &epb.RegionPlan{Id: "plan-01"}}, nil
		case "apply":
			if !request.Confirm || !request.DNSAuthorized || request.PlanID != "plan-01" || request.ExpectedRevision != "observed-revision" {
				t.Error("host-derived DNS authorization or plan confirmation was lost")
			}
			return helper.Response{Operation: &epb.RegionOperation{Id: "operation-01"}}, nil
		case "confirm", "rollback", "operation":
			if request.OperationID != "operation-01" {
				t.Error("operation scope was lost")
			}
			return helper.Response{Operation: &epb.RegionOperation{Id: "operation-01"}}, nil
		default:
			t.Errorf("unexpected generic helper action %q", action)
			return helper.Response{}, errors.New("unexpected action")
		}
	}})
	ctx := context.Background()
	if _, err := worker.InspectRegion(ctx, &epb.RegionInspectRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.PlanRegion(ctx, &epb.RegionPlanRequest{Config: &epb.RegionConfig{Country: "JP"}, RequestId: "request-plan-01", ExpectedRevision: "observed-revision"}); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.ApplyRegion(ctx, &epb.RegionApplyRequest{PlanId: "plan-01", ExpectedRevision: "observed-revision", RequestId: "request-apply-01", Confirm: true, DnsAuthorized: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.ConfirmRegion(ctx, &epb.RegionOperationRequest{OperationId: "operation-01", RequestId: "request-confirm-01", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RollbackRegion(ctx, &epb.RegionOperationRequest{OperationId: "operation-01", RequestId: "request-rollback-01", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.GetRegionOperation(ctx, &epb.RegionOperationRequest{OperationId: "operation-01"}); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 6 {
		t.Fatalf("unexpected helper dispatches: %v", actions)
	}
}

func TestRegionStopCancelsCallsWithoutOwningRootGuard(t *testing.T) {
	entered := make(chan struct{})
	worker := runningWorker(t, recordingHelper{call: func(ctx context.Context, action string, _ helper.Request) (helper.Response, error) {
		if action != "apply" {
			t.Errorf("stop tried root helper action %q", action)
		}
		close(entered)
		<-ctx.Done()
		return helper.Response{}, ctx.Err()
	}})
	result := make(chan error, 1)
	go func() { _, err := worker.ApplyRegion(context.Background(), &epb.RegionApplyRequest{}); result <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("worker did not propagate lifecycle cancellation: %v", err)
	}
	if _, err := worker.InspectRegion(context.Background(), &epb.RegionInspectRequest{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("stopped plugin admitted inspection: %v", err)
	}
}

func TestRegionErrorsAndVantageRemainExplicit(t *testing.T) {
	worker := runningWorker(t, recordingHelper{call: func(context.Context, string, helper.Request) (helper.Response, error) {
		return helper.Response{Code: "aborted", Error: "region revision changed"}, nil
	}})
	if _, err := worker.PlanRegion(context.Background(), &epb.RegionPlanRequest{}); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("revision error was changed: %v", err)
	}
	if _, err := worker.DiagnoseRegion(context.Background(), &epb.DiagnosticRequest{ProfileId: "browser-profile"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("region diagnostics impersonated a browser vantage")
	}
	if worker.Info().Name != "region" || worker.Info().Side != "server" {
		t.Fatal("region metadata exposes the wrong plugin side")
	}
}

func TestRegionDiagnosticComparisonDoesNotPromoteMissingEvidence(t *testing.T) {
	for _, test := range []struct {
		name, actual, state string
	}{
		{"matching", "Asia/Tokyo", "observed"},
		{"different", "Etc/UTC", "warning"},
		{"missing", "", "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			worker := runningWorker(t, recordingHelper{call: func(context.Context, string, helper.Request) (helper.Response, error) {
				return helper.Response{Status: &epb.RegionStatus{
					Actual:  &epb.RegionConfig{Timezone: test.actual},
					Desired: &epb.RegionConfig{Timezone: "Asia/Tokyo"},
					State:   "ready", ObservedAt: time.Now().Unix(),
				}}, nil
			}})
			report, err := worker.DiagnoseRegion(context.Background(), &epb.DiagnosticRequest{})
			if err != nil {
				t.Fatal(err)
			}
			for _, observation := range report.Observations {
				if observation.Check == "region.timezone" {
					if observation.State != test.state || observation.Value != test.actual || observation.Expected != "Asia/Tokyo" || observation.Vantage != "host" {
						t.Fatalf("configuration evidence was misclassified: %+v", observation)
					}
					return
				}
			}
			t.Fatal("configured timezone comparison missing")
		})
	}
}
