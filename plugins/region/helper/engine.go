package helper

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/proto"
)

const maxPlans = 512
const maxOperations = 512
const maxRequests = 4096

type Options struct {
	PlanTTL    time.Duration
	ConfirmTTL time.Duration
	Now        func() time.Time
}

type PlanRecord struct {
	Plan *epb.RegionPlan `json:"plan"`
}
type RequestRecord struct {
	Digest string `json:"digest"`
	Result string `json:"result"`
}
type ResolverPersistence struct {
	OperationID string               `json:"operation_id"`
	Manager     string               `json:"manager"`
	Record      FileState            `json:"record"`
	Ownership   map[string]FileState `json:"ownership"`
	BootID      string               `json:"boot_id"`
}
type OperationRecord struct {
	Operation           *epb.RegionOperation `json:"operation"`
	Config              *epb.RegionConfig    `json:"config"`
	BeforeDesired       *epb.RegionConfig    `json:"before_desired"`
	BeforeOwner         string               `json:"before_owner,omitempty"`
	BeforeResolver      *ResolverPersistence `json:"before_resolver,omitempty"`
	LeaseBootID         string               `json:"lease_boot_id,omitempty"`
	LeaseUntilBootNanos int64                `json:"lease_until_boot_nanos,omitempty"`
	Transition          Transition           `json:"transition"`
	AfterFingerprint    string               `json:"after_fingerprint,omitempty"`
	ChecksPassed        bool                 `json:"checks_passed"`
	RestoreReason       string               `json:"restore_reason,omitempty"`
	Applied             int                  `json:"applied"`
}

type Journal struct {
	Version int               `json:"version"`
	Revoked bool              `json:"revoked,omitempty"`
	Serial  uint64            `json:"serial"`
	Desired *epb.RegionConfig `json:"desired,omitempty"`
	// Owner is persisted before the first mutation, not at desired publication.
	// Equal configurations from different operations never share ownership.
	Owner            string                      `json:"owner,omitempty"`
	Plans            map[string]*PlanRecord      `json:"plans"`
	Operations       map[string]*OperationRecord `json:"operations"`
	Requests         map[string]RequestRecord    `json:"requests"`
	Pending          string                      `json:"pending,omitempty"`
	RecoveryProblem  string                      `json:"recovery_problem,omitempty"`
	GuardHeartbeat   int64                       `json:"guard_heartbeat"`
	Resolver         *ResolverPersistence        `json:"resolver,omitempty"`
	ResolverRecovery string                      `json:"resolver_recovery,omitempty"`
}

func (j *Journal) initialize() error {
	if j.Version == 0 {
		if j.Revoked || j.Serial != 0 || j.Desired != nil || j.Owner != "" || len(j.Plans) != 0 || len(j.Operations) != 0 || len(j.Requests) != 0 || j.Pending != "" || j.RecoveryProblem != "" || j.GuardHeartbeat != 0 || j.Resolver != nil || j.ResolverRecovery != "" {
			return fail("failed_precondition", "unversioned region journal contains state; administrator review is required")
		}
		j.Version = 2
	} else if j.Version != 2 {
		return fail("failed_precondition", "unsupported region journal version; administrator review is required, ownership cannot be inferred")
	}
	if j.Plans == nil {
		j.Plans = make(map[string]*PlanRecord)
	}
	if j.Operations == nil {
		j.Operations = make(map[string]*OperationRecord)
	}
	if j.Requests == nil {
		j.Requests = make(map[string]RequestRecord)
	}
	for id, record := range j.Operations {
		if record == nil || record.Operation == nil || record.Operation.Id != id || record.Config == nil {
			return fail("failed_precondition", "inconsistent region operation snapshot; administrator review is required")
		}
		if record.BeforeOwner == "" {
			if record.BeforeDesired != nil {
				return fail("failed_precondition", "region snapshot has desired state without transaction ownership")
			}
		} else {
			before := j.Operations[record.BeforeOwner]
			if before == nil || before == record || !proto.Equal(record.BeforeDesired, before.Config) {
				return fail("failed_precondition", "inconsistent previous region transaction ownership")
			}
		}
	}
	if j.Owner == "" {
		if j.Desired != nil || j.Pending != "" || j.RecoveryProblem != "" {
			return fail("failed_precondition", "region state has no owning transaction; administrator review is required")
		}
		return nil
	}
	owner := j.Operations[j.Owner]
	if owner == nil || owner.Operation.State == "rolled_back" || (j.Pending != "" && j.Pending != j.Owner) || (j.RecoveryProblem != "" && j.RecoveryProblem != j.Owner) {
		return fail("failed_precondition", "inconsistent current region transaction ownership")
	}
	// Only the current ancestry can regain ownership through rollback. Verify
	// it before any mutation, including rejecting cyclic or retired owners.
	for id, remaining := j.Owner, len(j.Operations); id != ""; remaining-- {
		if remaining == 0 {
			return fail("failed_precondition", "cyclic region transaction ownership")
		}
		record := j.Operations[id]
		if id != j.Owner && record.Operation.State != "applied" && record.Operation.State != "confirmed" {
			return fail("failed_precondition", "region predecessor no longer owns a completed transaction")
		}
		id = record.BeforeOwner
	}
	switch owner.Operation.State {
	case "applied", "confirmed", "checking_dns", "awaiting_confirmation":
		if !proto.Equal(j.Desired, owner.Config) {
			return fail("failed_precondition", "published region state does not match its owning transaction")
		}
	case "applying", "rolling_back", "rollback_failed", "rollback_conflict":
		if !proto.Equal(j.Desired, owner.Config) && !proto.Equal(j.Desired, owner.BeforeDesired) {
			return fail("failed_precondition", "recovering region state does not match its owning transaction")
		}
	default:
		return fail("failed_precondition", "invalid owning region operation state")
	}
	switch owner.Operation.State {
	case "applied", "confirmed":
		if j.Pending != "" || j.RecoveryProblem != "" {
			return fail("failed_precondition", "completed region transaction retains an inconsistent recovery lease")
		}
	case "rollback_conflict":
		if j.Pending != "" || j.RecoveryProblem != j.Owner {
			return fail("failed_precondition", "conflicted region transaction has inconsistent recovery ownership")
		}
	default:
		if j.Pending != j.Owner {
			return fail("failed_precondition", "incomplete region transaction has no matching recovery lease")
		}
	}
	if j.RecoveryProblem != "" && owner.Operation.State != "rollback_conflict" && owner.Operation.State != "rollback_failed" && owner.Operation.State != "rolling_back" {
		return fail("failed_precondition", "region recovery problem does not match the owning operation state")
	}
	return nil
}

type Engine struct {
	store   Store
	backend Backend
	options Options
}

func NewEngine(store Store, backend Backend, options Options) *Engine {
	if options.PlanTTL == 0 {
		options.PlanTTL = 5 * time.Minute
	}
	if options.ConfirmTTL == 0 {
		options.ConfirmTTL = 2 * time.Minute
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Engine{store: store, backend: backend, options: options}
}

func (e *Engine) Handle(ctx context.Context, action string, request Request) Response {
	var response Response
	if err := validateActionRequest(action, request); err != nil {
		return errorResponse(err)
	}
	err := e.store.WithLock(ctx, func(j *Journal, save func() error) error {
		if err := j.initialize(); err != nil {
			return err
		}
		if j.Revoked && action != "inspect" && action != "operation" && action != "prepare-uninstall" && action != "activate" {
			return fail("permission_denied", "region helper authorization has been quiesced by the administrator")
		}
		if err := e.recoverExpired(ctx, j, save); err != nil {
			return err
		}
		switch action {
		case "inspect":
			status, err := e.inspect(ctx, j)
			response.Status = status
			return err
		case "prepare-uninstall":
			j.Revoked = true
			if err := save(); err != nil {
				return err
			}
			// A conflict clears the timed lease, but must remain recoverable by
			// an administrator after the external edit has been resolved.
			if j.Pending == "" {
				record := j.Operations[j.RecoveryProblem]
				if record != nil && j.Owner == record.Operation.Id {
					j.Pending = record.Operation.Id
				}
			}
			if record := j.Operations[j.Pending]; record != nil {
				record.RestoreReason = "Administrator uninstall canceled an unconfirmed region lease"
				if err := e.restore(ctx, j, save, record); err != nil {
					return err
				}
			}
			status, err := e.inspect(ctx, j)
			response.Status = status
			return err
		case "activate":
			j.Revoked = false
			if err := save(); err != nil {
				return err
			}
			status, err := e.inspect(ctx, j)
			response.Status = status
			return err
		case "plan":
			plan, err := e.plan(ctx, j, save, request)
			response.Plan = plan
			return err
		case "apply":
			operation, err := e.apply(ctx, j, save, request)
			response.Operation = operation
			return err
		case "confirm", "rollback", "operation":
			operation, err := e.operation(ctx, j, save, action, request)
			response.Operation = operation
			return err
		default:
			return fail("invalid_argument", "unsupported region action")
		}
	})
	if err != nil {
		return errorResponse(err)
	}
	return response
}

func errorResponse(err error) Response {
	var typed *Error
	if errors.As(err, &typed) {
		return Response{Code: typed.Code, Error: typed.Message}
	}
	if errors.Is(err, context.Canceled) {
		return Response{Code: "canceled", Error: "region request canceled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Response{Code: "deadline_exceeded", Error: "region request deadline exceeded"}
	}
	return Response{Code: "internal", Error: err.Error()}
}

func validateActionRequest(action string, r Request) error {
	switch action {
	case "inspect", "prepare-uninstall", "activate":
		if r.Config != nil || r.ExpectedRevision != "" || r.RequestID != "" || r.PlanID != "" || r.OperationID != "" || r.Confirm || r.DNSAuthorized {
			return fail("invalid_argument", "inspect takes an empty JSON object")
		}
	case "plan":
		if r.PlanID != "" || r.OperationID != "" || r.Confirm || r.DNSAuthorized {
			return fail("invalid_argument", "unexpected plan fields")
		}
		if err := validID(r.RequestID, "request_id"); err != nil {
			return err
		}
		if r.ExpectedRevision == "" {
			return fail("invalid_argument", "expected_revision is required")
		}
	case "apply":
		if r.Config != nil || r.OperationID != "" {
			return fail("invalid_argument", "unexpected apply fields")
		}
		if err := validID(r.RequestID, "request_id"); err != nil {
			return err
		}
		if err := validID(r.PlanID, "plan_id"); err != nil {
			return err
		}
		if !r.Confirm {
			return fail("failed_precondition", "explicit plan application confirmation is required")
		}
	case "confirm", "rollback", "operation":
		if r.Config != nil || r.ExpectedRevision != "" || r.PlanID != "" || r.DNSAuthorized {
			return fail("invalid_argument", "unexpected operation fields")
		}
		if err := validID(r.OperationID, "operation_id"); err != nil {
			return err
		}
		if action != "operation" {
			if err := validID(r.RequestID, "request_id"); err != nil {
				return err
			}
			if !r.Confirm {
				return fail("failed_precondition", "explicit operation confirmation is required")
			}
		} else if r.RequestID != "" || r.Confirm {
			return fail("invalid_argument", "operation inspection is read-only")
		}
	default:
		return fail("invalid_argument", "unsupported region action")
	}
	return nil
}

func (e *Engine) inspect(ctx context.Context, j *Journal) (*epb.RegionStatus, error) {
	observed, err := e.backend.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	status := &epb.RegionStatus{Actual: observed.Actual, Desired: cloneConfig(j.Desired), Revision: revision(j, observed), DnsManager: observed.Manager, HelperAvailable: true, ObservedAt: e.options.Now().Unix(), State: "ready", Warnings: append([]string(nil), observed.Warnings...)}
	for _, dependency := range observed.Missing {
		status.Warnings = append(status.Warnings, "Missing administrator-installed dependency: "+dependency+"; no packages are installed by this plugin")
	}
	if !e.guardFresh(j) {
		status.Warnings = append(status.Warnings, "Root recovery guard is not ready; applying changes is disabled")
	}
	if j.Revoked {
		status.State = "disabled"
		status.Warnings = append(status.Warnings, "Administrator quiesced region mutation authorization; only root activate can reopen it")
	}
	if j.ResolverRecovery != "" {
		status.State = "dns_recovery_conflict"
		if j.Resolver != nil {
			status.PendingOperationId = j.Resolver.OperationID
		}
		status.Warnings = append(status.Warnings, j.ResolverRecovery)
	}
	if problem := j.Operations[j.RecoveryProblem]; problem != nil {
		status.State = problem.Operation.State
		status.Warnings = append(status.Warnings, "Root recovery requires administrator review: "+problem.Operation.Id+": "+problem.Operation.Reason)
	}
	if j.Pending != "" {
		if operation := j.Operations[j.Pending]; operation != nil {
			status.State = operation.Operation.State
			status.PendingOperationId = j.Pending
			status.ConfirmBefore = operation.Operation.ConfirmBefore
			status.Warnings = append(status.Warnings, "Pending region operation: "+j.Pending)
		}
	}
	status.Warnings = append(status.Warnings, "Existing applications, shells, tmux panes and Chrome are not restarted; user/session overrides may differ from system defaults")
	return status, nil
}

func revision(j *Journal, observed Observation) string {
	return digest(struct {
		Serial               uint64
		Actual               *epb.RegionConfig
		Desired              *epb.RegionConfig
		Fingerprint, Manager string
	}{j.Serial, observed.Actual, j.Desired, observed.Fingerprint, observed.Manager})
}
func (e *Engine) guardFresh(j *Journal) bool {
	age := e.options.Now().Unix() - j.GuardHeartbeat
	return j.GuardHeartbeat != 0 && age >= 0 && age <= 10
}
func randomID(prefix string) (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(data[:]), nil
}
func deduplicate(j *Journal, action string, r Request) (string, bool, error) {
	if old, ok := j.Requests[action+"/"+r.RequestID]; ok {
		if old.Digest != requestDigest(action, r) {
			return "", false, fail("already_exists", "request_id was already used with different region request content")
		}
		return old.Result, true, nil
	}
	if len(j.Requests) >= maxRequests {
		return "", false, fail("resource_exhausted", "root region request journal is full; administrator archival is required")
	}
	return "", false, nil
}
func remember(j *Journal, action string, r Request, result string) {
	j.Requests[action+"/"+r.RequestID] = RequestRecord{Digest: requestDigest(action, r), Result: result}
}

func (e *Engine) plan(ctx context.Context, j *Journal, save func() error, r Request) (*epb.RegionPlan, error) {
	if id, found, err := deduplicate(j, "plan", r); err != nil {
		return nil, err
	} else if found {
		return proto.Clone(j.Plans[id].Plan).(*epb.RegionPlan), nil
	}
	config, err := NormalizeConfig(r.Config)
	if err != nil {
		return nil, err
	}
	changesDNS := config.Dns.ManageSystem
	if previous := j.Desired.GetDns(); previous != nil {
		if !changesDNS && previous.ManageSystem {
			config.Dns.ManageSystem = true
			config.Dns.Servers = append([]string(nil), previous.Servers...)
			config.Dns.CheckDomains = append([]string(nil), previous.CheckDomains...)
		}
		if config.Dns.ChromeMode == "unchanged" {
			config.Dns.ChromeMode = previous.ChromeMode
			config.Dns.ChromeTemplates = append([]string(nil), previous.ChromeTemplates...)
		}
	}
	observed, err := e.backend.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	current := revision(j, observed)
	if r.ExpectedRevision != current {
		return nil, fail("aborted", "region revision changed; inspect and create a new plan")
	}
	if len(j.Plans) >= maxPlans {
		return nil, fail("resource_exhausted", "root region plan journal is full; administrator archival is required")
	}
	id, err := randomID("plan-")
	if err != nil {
		return nil, err
	}
	beforeChanges := cloneConfig(observed.Actual)
	if previous := j.Desired.GetDns(); previous != nil {
		if beforeChanges.Dns == nil {
			beforeChanges.Dns = &epb.DNSConfig{}
		}
		beforeChanges.Dns.ChromeMode = previous.ChromeMode
		beforeChanges.Dns.ChromeTemplates = append([]string(nil), previous.ChromeTemplates...)
	}
	plan := &epb.RegionPlan{Id: id, Revision: current, Config: config, Changes: settingChanges(beforeChanges, config), ChangesDns: changesDNS, ExpiresAt: e.options.Now().Add(e.options.PlanTTL).Unix(), Warnings: append([]string(nil), observed.Warnings...)}
	for _, dependency := range observed.Missing {
		plan.Warnings = append(plan.Warnings, "Missing administrator-installed dependency: "+dependency)
	}
	if _, err := e.backend.Prepare(ctx, mutationConfig(config, changesDNS)); err != nil {
		var typed *Error
		if errors.As(err, &typed) && typed.Code == "failed_precondition" {
			plan.Warnings = append(plan.Warnings, "Application blocked: "+typed.Message)
		} else {
			return nil, err
		}
	}
	plan.Warnings = append(plan.Warnings, "No package installation, real clock/NTP/route change, or application restart is performed", "Country is a configuration preset, not proof of identity, residence, exit IP, or service eligibility")
	if plan.ChangesDns {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("System DNS is separately authorized; fresh DNS and TLS checks contact only the listed domains; confirm within %d seconds or the independent root guard attempts rollback", int(e.options.ConfirmTTL.Seconds())))
	}
	if config.Dns.ChromeMode != "unchanged" {
		plan.Warnings = append(plan.Warnings, "Chrome secure DNS applies only to a subsequent managed Chrome launch; an existing browser remains unchanged")
	}
	j.Plans[id] = &PlanRecord{Plan: plan}
	remember(j, "plan", r, id)
	if err := save(); err != nil {
		return nil, err
	}
	return proto.Clone(plan).(*epb.RegionPlan), nil
}

func (e *Engine) apply(ctx context.Context, j *Journal, save func() error, r Request) (*epb.RegionOperation, error) {
	if id, found, err := deduplicate(j, "apply", r); err != nil {
		return nil, err
	} else if found {
		return cloneOperation(j.Operations[id]), nil
	}
	planRecord := j.Plans[r.PlanID]
	if planRecord == nil {
		return nil, fail("not_found", "region plan not found")
	}
	plan := planRecord.Plan
	if e.options.Now().Unix() >= plan.ExpiresAt {
		return nil, fail("failed_precondition", "region plan expired; create a new plan")
	}
	if r.ExpectedRevision != plan.Revision {
		return nil, fail("aborted", "application must name the exact planned revision")
	}
	observed, err := e.backend.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	if revision(j, observed) != plan.Revision {
		return nil, fail("aborted", "system region changed since planning")
	}
	if j.Pending != "" || j.RecoveryProblem != "" {
		return nil, fail("failed_precondition", "a region operation is already awaiting confirmation or administrator recovery")
	}
	if !e.guardFresh(j) {
		return nil, fail("failed_precondition", "independent root recovery guard is unavailable")
	}
	if plan.ChangesDns && !r.DNSAuthorized {
		return nil, fail("permission_denied", "separate system DNS permission is required")
	}
	if len(j.Operations) >= maxOperations {
		return nil, fail("resource_exhausted", "root region operation journal is full; administrator archival is required")
	}
	transition, err := e.backend.Prepare(ctx, mutationConfig(plan.Config, plan.ChangesDns))
	if err != nil {
		return nil, err
	}
	id, err := randomID("operation-")
	if err != nil {
		return nil, err
	}
	deadline := e.options.Now().Add(e.options.ConfirmTTL).Unix()
	leaseBoot, err := e.backend.BootIdentity(ctx)
	if err != nil {
		return nil, err
	}
	leaseElapsed, err := e.backend.BootElapsed(ctx)
	if err != nil {
		return nil, err
	}
	record := &OperationRecord{Operation: &epb.RegionOperation{Id: id, State: "applying", ConfirmBefore: deadline, RestartRequired: true, Warnings: []string{"Existing applications are unchanged and require an explicit restart to adopt new defaults"}}, Config: cloneConfig(plan.Config), BeforeDesired: cloneConfig(j.Desired), Transition: transition}
	record.BeforeResolver = cloneResolver(j.Resolver)
	record.BeforeOwner = j.Owner
	record.LeaseBootID, record.LeaseUntilBootNanos = leaseBoot, leaseElapsed+e.options.ConfirmTTL.Nanoseconds()
	j.Operations[id], j.Pending, j.Owner = record, id, id
	j.RecoveryProblem = ""
	remember(j, "apply", r, id)
	// The complete before/intended-after snapshots and recovery lease reach
	// root-owned durable storage before the first write, including crash gaps.
	if err := save(); err != nil {
		return nil, err
	}
	for index, mutation := range transition.Entries {
		current, readErr := e.backend.Read(ctx, mutation.Key)
		if readErr != nil {
			return e.failedApply(ctx, j, save, record, readErr)
		}
		if !sameFile(current, mutation.Before) {
			return e.failedApply(ctx, j, save, record, fail("aborted", "external host change occurred after snapshot preparation"))
		}
		if err := e.backend.Write(ctx, mutation.Key, mutation.After); err != nil {
			return e.failedApply(ctx, j, save, record, err)
		}
		record.Applied = index + 1
		if err := save(); err != nil {
			return nil, err
		}
	}
	if err := e.backend.Reload(ctx, transition); err != nil {
		return e.failedApply(ctx, j, save, record, err)
	}
	observed, err = e.backend.Inspect(ctx)
	if err != nil {
		return e.failedApply(ctx, j, save, record, err)
	}
	record.AfterFingerprint = observed.Fingerprint
	j.Desired = cloneConfig(record.Config)
	j.Serial++
	record.Operation.Revision = revision(j, observed)
	if transition.DNS {
		record.Operation.State = "checking_dns"
		if err := save(); err != nil {
			return nil, err
		}
		warnings, checkErr := e.backend.CheckDNS(ctx, record.Config)
		record.Operation.Warnings = append(record.Operation.Warnings, warnings...)
		if checkErr != nil {
			return e.failedApply(ctx, j, save, record, checkErr)
		}
		record.ChecksPassed = true
		record.Operation.State = "awaiting_confirmation"
		record.Operation.Reason = "Fresh system DNS resolution and new TLS connections passed; explicit confirmation is still required"
	} else {
		record.Operation.State = "applied"
		record.Operation.ConfirmBefore = 0
		j.Pending = ""
	}
	if err := save(); err != nil {
		return nil, err
	}
	return cloneOperation(record), nil
}

func cloneOperation(record *OperationRecord) *epb.RegionOperation {
	return proto.Clone(record.Operation).(*epb.RegionOperation)
}
func (e *Engine) failedApply(ctx context.Context, j *Journal, save func() error, record *OperationRecord, cause error) (*epb.RegionOperation, error) {
	record.RestoreReason = "Application/check failed: " + cause.Error()
	// Recovery does not inherit a canceled client context. The independent
	// guard retries if this bounded recovery cannot complete.
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := e.restore(recovery, j, save, record); err != nil {
		return nil, err
	}
	return cloneOperation(record), nil
}

func (e *Engine) operation(ctx context.Context, j *Journal, save func() error, action string, r Request) (*epb.RegionOperation, error) {
	record := j.Operations[r.OperationID]
	if record == nil {
		return nil, fail("not_found", "region operation not found")
	}
	if action == "operation" {
		return cloneOperation(record), nil
	}
	if id, found, err := deduplicate(j, action, r); err != nil {
		return nil, err
	} else if found {
		return cloneOperation(j.Operations[id]), nil
	}
	switch action {
	case "confirm":
		if record.Operation.State != "awaiting_confirmation" || j.Pending != r.OperationID || j.Owner != r.OperationID || !record.ChecksPassed {
			return nil, fail("failed_precondition", "operation is not awaiting a successful DNS confirmation")
		}
		observed, err := e.backend.Inspect(ctx)
		if err != nil {
			return nil, err
		}
		if observed.Fingerprint != record.AfterFingerprint {
			return nil, fail("aborted", "host configuration changed after DNS application; refusing confirmation")
		}
		record.Operation.State, record.Operation.Reason, record.Operation.ConfirmBefore = "confirmed", "User confirmed after fresh DNS and TLS checks", 0
		persistence, err := e.resolverPersistence(ctx, record)
		if err != nil {
			return nil, err
		}
		j.Resolver, j.ResolverRecovery = persistence, ""
		j.Pending = ""
	case "rollback":
		if record.Operation.State == "rolled_back" {
			break
		}
		if record.Operation.State == "rollback_conflict" {
			return nil, fail("aborted", "rollback conflicted with external host changes; administrator review is required")
		}
		if j.Pending != "" && j.Pending != r.OperationID {
			return nil, fail("failed_precondition", "another operation holds the root writer lease")
		}
		if j.Owner != r.OperationID {
			return nil, fail("aborted", "a newer region transaction owns the host configuration; refusing stale rollback")
		}
		j.Pending = r.OperationID
		record.RestoreReason = "Explicit user rollback"
		if err := save(); err != nil {
			return nil, err
		}
		if err := e.restore(ctx, j, save, record); err != nil {
			return nil, err
		}
	}
	remember(j, action, r, r.OperationID)
	if err := save(); err != nil {
		return nil, err
	}
	return cloneOperation(record), nil
}

func sameFile(a, b FileState) bool {
	return a.Kind == b.Kind && a.Target == b.Target && a.Mode == b.Mode && bytes.Equal(a.Data, b.Data)
}
func (e *Engine) restore(ctx context.Context, j *Journal, save func() error, record *OperationRecord) error {
	if j.Owner != record.Operation.Id || j.Pending != record.Operation.Id {
		return fail("aborted", "region recovery does not own the current transaction")
	}
	conflicts := make([]string, 0)
	if record.Transition.DNS {
		observed, err := e.backend.Inspect(ctx)
		if err != nil {
			return e.restoreFailed(j, save, record, err)
		}
		if observed.Manager != record.Transition.Manager && !(record.Transition.Manager == "systemd-resolved" && observed.Manager == "systemd-resolved-inactive") {
			conflicts = append(conflicts, "DNS owner changed from "+record.Transition.Manager+" to "+observed.Manager)
		}
	}
	// First validate EVERY target before touching any, so an administrator's
	// modification is never overwritten even when another target still matches.
	for key, expected := range record.Transition.Ownership {
		current, err := e.backend.Read(ctx, key)
		if err != nil {
			return e.restoreFailed(j, save, record, err)
		}
		if !sameFile(current, expected) {
			conflicts = append(conflicts, key)
		}
	}
	for _, mutation := range record.Transition.Entries {
		current, err := e.backend.Read(ctx, mutation.Key)
		if err != nil {
			return e.restoreFailed(j, save, record, err)
		}
		if !sameFile(current, mutation.After) && !sameFile(current, mutation.Before) {
			conflicts = append(conflicts, mutation.Key)
		}
	}
	if len(conflicts) != 0 {
		record.Operation.State, record.Operation.Reason = "rollback_conflict", "External changes prevent safe rollback: "+strings.Join(conflicts, ", ")
		record.Operation.Warnings = append(record.Operation.Warnings, "No rollback target was overwritten; administrator recovery is required")
		record.Operation.ConfirmBefore, j.Pending = 0, ""
		j.RecoveryProblem = record.Operation.Id
		return save()
	}
	record.Operation.State = "rolling_back"
	if err := save(); err != nil {
		return err
	}
	for index := len(record.Transition.Entries) - 1; index >= 0; index-- {
		mutation := record.Transition.Entries[index]
		current, err := e.backend.Read(ctx, mutation.Key)
		if err != nil {
			return e.restoreFailed(j, save, record, err)
		}
		if sameFile(current, mutation.Before) {
			continue
		}
		if !sameFile(current, mutation.After) {
			return e.restoreFailed(j, save, record, fail("aborted", "external change occurred during rollback"))
		}
		if err := e.backend.Write(ctx, mutation.Key, mutation.Before); err != nil {
			return e.restoreFailed(j, save, record, err)
		}
	}
	if err := e.backend.Reload(ctx, record.Transition); err != nil {
		return e.restoreFailed(j, save, record, err)
	}
	observed, err := e.backend.Inspect(ctx)
	if err != nil {
		return e.restoreFailed(j, save, record, err)
	}
	j.Desired, j.Owner = cloneConfig(record.BeforeDesired), record.BeforeOwner
	if record.Transition.DNS {
		j.Resolver = cloneResolver(record.BeforeResolver)
		j.ResolverRecovery = ""
	}
	j.Serial++
	record.Operation.State, record.Operation.Reason, record.Operation.ConfirmBefore = "rolled_back", record.RestoreReason, 0
	record.Operation.Revision = revision(j, observed)
	j.Pending = ""
	if j.RecoveryProblem == record.Operation.Id {
		j.RecoveryProblem = ""
	}
	return save()
}
func (e *Engine) restoreFailed(j *Journal, save func() error, record *OperationRecord, err error) error {
	record.Operation.State, record.Operation.Reason = "rollback_failed", "Root recovery failed and will retry: "+err.Error()
	j.RecoveryProblem = record.Operation.Id
	if saveErr := save(); saveErr != nil {
		return saveErr
	}
	return nil
}
func (e *Engine) recoverExpired(ctx context.Context, j *Journal, save func() error) error {
	if j.Pending == "" {
		return nil
	}
	record := j.Operations[j.Pending]
	if record == nil {
		return fail("internal", "root writer lease has no operation snapshot")
	}
	bootExpired := false
	if record.LeaseBootID != "" {
		boot, err := e.backend.BootIdentity(ctx)
		if err != nil {
			return err
		}
		elapsed, err := e.backend.BootElapsed(ctx)
		if err != nil {
			return err
		}
		bootExpired = boot != record.LeaseBootID || elapsed >= record.LeaseUntilBootNanos
	}
	if record.Operation.State == "rolling_back" || record.Operation.State == "rollback_failed" || e.options.Now().Unix() >= record.Operation.ConfirmBefore || bootExpired {
		if record.RestoreReason == "" {
			record.RestoreReason = "Independent root recovery: confirmation/application lease expired"
		}
		return e.restore(ctx, j, save, record)
	}
	return nil
}

// GuardTick is called by the installed root systemd service, not the plugin.
// It persists a heartbeat only after it successfully holds the same writer lock.
func (e *Engine) GuardTick(ctx context.Context) error {
	return e.store.WithLock(ctx, func(j *Journal, save func() error) error {
		if err := j.initialize(); err != nil {
			return err
		}
		if err := e.recoverExpired(ctx, j, save); err != nil {
			return err
		}
		j.GuardHeartbeat = e.options.Now().Unix()
		if !j.Revoked {
			if err := e.recoverResolverBoot(ctx, j); err != nil {
				j.ResolverRecovery = "Confirmed resolvconf boot recovery is pending: " + err.Error()
			}
		}
		return save()
	})
}

func mutationConfig(config *epb.RegionConfig, dns bool) *epb.RegionConfig {
	copy := cloneConfig(config)
	copy.Dns.ManageSystem = dns
	return copy
}
func cloneResolver(value *ResolverPersistence) *ResolverPersistence {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Ownership = make(map[string]FileState, len(value.Ownership))
	for key, state := range value.Ownership {
		copy.Ownership[key] = state
	}
	return &copy
}
func (e *Engine) resolverPersistence(ctx context.Context, record *OperationRecord) (*ResolverPersistence, error) {
	manager := record.Transition.Manager
	if manager != "resolvconf" && manager != "openresolv" {
		return nil, nil
	}
	boot, err := e.backend.BootIdentity(ctx)
	if err != nil {
		return nil, err
	}
	persistence := &ResolverPersistence{OperationID: record.Operation.Id, Manager: manager, BootID: boot, Ownership: make(map[string]FileState)}
	for _, key := range []string{"resolver", "resolvconf-head", "resolvconf-tail"} {
		state, err := e.backend.Read(ctx, key)
		if err != nil {
			return nil, err
		}
		persistence.Ownership[key] = state
	}
	if manager == "resolvconf" {
		for _, key := range []string{"resolvconf-order", "resolvconf-owner"} {
			state, err := e.backend.Read(ctx, key)
			if err != nil {
				return nil, err
			}
			persistence.Ownership[key] = state
		}
	}
	persistence.Record, err = e.backend.Read(ctx, "resolvconf-record")
	return persistence, err
}
func (e *Engine) recoverResolverBoot(ctx context.Context, j *Journal) error {
	if j.Pending != "" || j.Resolver == nil {
		return nil
	}
	boot, err := e.backend.BootIdentity(ctx)
	if err != nil {
		return err
	}
	if boot == j.Resolver.BootID {
		return nil
	}
	observed, err := e.backend.Inspect(ctx)
	if err != nil {
		return err
	}
	if observed.Manager != j.Resolver.Manager {
		j.Resolver.BootID = boot
		j.ResolverRecovery = "Confirmed resolvconf boot recovery refused an externally changed DNS owner"
		return nil
	}
	for key, expected := range j.Resolver.Ownership {
		current, err := e.backend.Read(ctx, key)
		if err != nil {
			return err
		}
		if !sameFile(current, expected) {
			j.Resolver.BootID = boot
			j.ResolverRecovery = "Confirmed resolvconf boot recovery refused an external owner/checkpoint change: " + key
			return nil
		}
	}
	current, err := e.backend.Read(ctx, "resolvconf-record")
	if err != nil {
		return err
	}
	if current.Kind != "absent" && !sameFile(current, j.Resolver.Record) {
		j.Resolver.BootID = boot
		j.ResolverRecovery = "Confirmed resolvconf boot recovery refused an externally changed resolver record"
		return nil
	}
	if current.Kind == "absent" {
		if err := e.backend.Write(ctx, "resolvconf-record", j.Resolver.Record); err != nil {
			return err
		}
	}
	if err := e.backend.Reload(ctx, Transition{DNS: true, Manager: j.Resolver.Manager}); err != nil {
		return err
	}
	j.Resolver.BootID, j.ResolverRecovery = boot, ""
	return nil
}
