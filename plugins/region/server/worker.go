package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/diagnostics"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/region/helper"
)

const Capability = "region.host.v1"

type Helper interface {
	Call(context.Context, string, helper.Request) (helper.Response, error)
}
type Worker struct {
	mu                  sync.Mutex
	helper              Helper
	initialized, closed bool
	lifetime            context.Context
	cancel              context.CancelFunc
	active              sync.WaitGroup
	inflight            int
	stopped             chan struct{}
}

func New() *Worker                        { return NewWithHelper(helper.Client{}) }
func NewWithHelper(client Helper) *Worker { return &Worker{helper: client} }
func (w *Worker) Info() *ppb.PluginInfo {
	return &ppb.PluginInfo{Name: "region", Version: buildinfo.Version, Description: "Versioned host timezone, UTF-8 locale and separately authorized guarded DNS", Side: "server", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", Capability}}}
}
func (w *Worker) Init(_ context.Context, in sdk.InitContext) error {
	if in.Side != "server" || !filepath.IsAbs(in.StateDir) || in.Core == nil || w.helper == nil {
		return connect.NewError(connect.CodeInvalidArgument, "invalid region server initialization")
	}
	if in.Config != nil && len(in.Config.Fields) != 0 {
		return connect.NewError(connect.CodeInvalidArgument, "region transactions use the typed plan API; helper policy is administrator-owned")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.initialized {
		return connect.NewError(connect.CodeFailedPrecondition, "region server already initialized")
	}
	w.initialized = true
	return nil
}
func (w *Worker) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.initialized || w.closed {
		return connect.NewError(connect.CodeFailedPrecondition, "region server is not initialized")
	}
	if w.lifetime != nil {
		if w.lifetime.Err() != nil {
			return connect.NewError(connect.CodeFailedPrecondition, "region server lifetime ended")
		}
		return nil
	}
	w.lifetime, w.cancel = context.WithCancel(ctx)
	return nil
}
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if w.stopped == nil {
		w.closed = true
		if w.cancel != nil {
			w.cancel()
		}
		w.stopped = make(chan struct{})
		go func() { w.active.Wait(); close(w.stopped) }()
	}
	done := w.stopped
	w.mu.Unlock()
	// Root DNS leases belong to the independent guard, not this lifecycle.
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (w *Worker) begin(parent context.Context) (context.Context, func(), error) {
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	w.mu.Lock()
	if w.closed || w.lifetime == nil || w.lifetime.Err() != nil {
		w.mu.Unlock()
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "region plugin is not running")
	}
	if w.inflight >= 16 {
		w.mu.Unlock()
		return nil, nil, connect.NewError(connect.CodeResourceExhausted, "region metadata operation limit reached")
	}
	w.inflight++
	w.active.Add(1)
	lifetime := w.lifetime
	w.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 85*time.Second)
	stop := context.AfterFunc(lifetime, cancel)
	return ctx, func() {
		stop()
		cancel()
		w.mu.Lock()
		w.inflight--
		w.mu.Unlock()
		w.active.Done()
	}, nil
}
func (w *Worker) Health(parent context.Context) (*pb.HealthStatus, error) {
	ctx, done, err := w.begin(parent)
	if err != nil {
		return &pb.HealthStatus{Status: pb.HealthStatus_UNHEALTHY, Message: "Region plugin is stopped or unavailable"}, nil
	}
	defer done()
	status, err := w.inspect(ctx)
	if err != nil {
		return &pb.HealthStatus{Status: pb.HealthStatus_DEGRADED, Message: "Region inspection failed; no successful host mutation is assumed"}, nil
	}
	health := pb.HealthStatus_HEALTHY
	if !status.HelperAvailable || status.State == "rollback_failed" || status.State == "rollback_conflict" || status.State == "unsupported_platform" || status.State == "disabled" || status.State == "dns_recovery_conflict" {
		health = pb.HealthStatus_DEGRADED
	}
	return &pb.HealthStatus{Status: health, Message: status.State}, nil
}
func responseError(response helper.Response, err error) error {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return connect.NewError(connect.CodeUnavailable, err.Error())
	}
	if response.Error == "" {
		return nil
	}
	codes := map[string]connect.Code{"invalid_argument": connect.CodeInvalidArgument, "failed_precondition": connect.CodeFailedPrecondition, "aborted": connect.CodeAborted, "not_found": connect.CodeNotFound, "already_exists": connect.CodeAlreadyExists, "permission_denied": connect.CodePermissionDenied, "resource_exhausted": connect.CodeResourceExhausted, "canceled": connect.CodeCanceled, "deadline_exceeded": connect.CodeDeadlineExceeded, "internal": connect.CodeInternal}
	code, ok := codes[response.Code]
	if !ok {
		code = connect.CodeInternal
	}
	return connect.NewError(code, response.Error)
}
func (w *Worker) inspect(ctx context.Context) (*epb.RegionStatus, error) {
	response, err := w.helper.Call(ctx, "inspect", helper.Request{})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		status, readErr := helper.InspectReadOnly(ctx)
		if readErr != nil {
			return nil, responseError(response, err)
		}
		return status, nil
	}
	if err := responseError(response, nil); err != nil {
		return nil, err
	}
	if response.Status == nil {
		return nil, connect.NewError(connect.CodeInternal, "region helper returned no status")
	}
	return response.Status, nil
}
func (w *Worker) InspectRegion(parent context.Context, request *epb.RegionInspectRequest) (*epb.RegionStatus, error) {
	if request == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "region inspect request is required")
	}
	ctx, done, err := w.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	return w.inspect(ctx)
}
func (w *Worker) PlanRegion(parent context.Context, request *epb.RegionPlanRequest) (*epb.RegionPlan, error) {
	if request == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "region plan request is required")
	}
	ctx, done, err := w.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	response, err := w.helper.Call(ctx, "plan", helper.Request{Config: request.Config, ExpectedRevision: request.ExpectedRevision, RequestID: request.RequestId})
	if err := responseError(response, err); err != nil {
		return nil, err
	}
	if response.Plan == nil {
		return nil, connect.NewError(connect.CodeInternal, "region helper returned no plan")
	}
	return response.Plan, nil
}
func (w *Worker) ApplyRegion(parent context.Context, request *epb.RegionApplyRequest) (*epb.RegionOperation, error) {
	if request == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "region apply request is required")
	}
	ctx, done, err := w.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	return w.callOperation(ctx, "apply", helper.Request{PlanID: request.PlanId, ExpectedRevision: request.ExpectedRevision, RequestID: request.RequestId, Confirm: request.Confirm, DNSAuthorized: request.DnsAuthorized})
}
func (w *Worker) callOperation(ctx context.Context, action string, request helper.Request) (*epb.RegionOperation, error) {
	response, err := w.helper.Call(ctx, action, request)
	if err := responseError(response, err); err != nil {
		return nil, err
	}
	if response.Operation == nil {
		return nil, connect.NewError(connect.CodeInternal, "region helper returned no operation")
	}
	return response.Operation, nil
}
func (w *Worker) operation(parent context.Context, action string, request *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	if request == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "region operation request is required")
	}
	ctx, done, err := w.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	return w.callOperation(ctx, action, helper.Request{OperationID: request.OperationId, RequestID: request.RequestId, Confirm: request.Confirm})
}
func (w *Worker) ConfirmRegion(ctx context.Context, request *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	return w.operation(ctx, "confirm", request)
}
func (w *Worker) RollbackRegion(ctx context.Context, request *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	return w.operation(ctx, "rollback", request)
}
func (w *Worker) GetRegionOperation(ctx context.Context, request *epb.RegionOperationRequest) (*epb.RegionOperation, error) {
	return w.operation(ctx, "operation", request)
}
func (w *Worker) DiagnoseRegion(parent context.Context, request *epb.DiagnosticRequest) (*epb.DiagnosticReport, error) {
	if request == nil || request.ProfileId != "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "region diagnostics are host-vantage only; use browser diagnostics for a profile")
	}
	ctx, done, err := w.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	report, err := diagnostics.InspectHost(ctx, request)
	if err != nil {
		return nil, err
	}
	status, err := w.inspect(ctx)
	if err != nil {
		report.Warnings = append(report.Warnings, "Privileged region desired/revision evidence unavailable; no successful application is inferred")
		return report, nil
	}
	report.Warnings = append(report.Warnings, status.Warnings...)
	for _, evidence := range []struct{ check, value, expected string }{
		{"region.revision", status.Revision, ""}, {"region.timezone", status.GetActual().GetTimezone(), status.GetDesired().GetTimezone()},
		{"region.locale", status.GetActual().GetLocale(), status.GetDesired().GetLocale()}, {"region.system_dns_owner", status.DnsManager, ""}, {"region.operation_state", status.State, "ready"},
	} {
		state := "observed"
		if evidence.value == "" {
			state = "unknown"
		} else if evidence.expected != "" && evidence.value != evidence.expected {
			state = "warning"
		}
		report.Observations = append(report.Observations, &epb.DiagnosticObservation{Check: evidence.check, State: state, Value: evidence.value, Expected: evidence.expected, Source: "region helper/system configuration inspection", Vantage: "host", ObservedAt: status.ObservedAt, Limitation: "Configuration evidence does not prove an existing process environment, browser profile, exit IP, physical location or third-party eligibility"})
	}
	return report, nil
}
