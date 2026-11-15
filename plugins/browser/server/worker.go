package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
	"google.golang.org/protobuf/proto"
)

const maxDatabaseBytes = 8 << 20

type receipt struct {
	Method    string          `json:"method"`
	Digest    string          `json:"digest"`
	Result    json.RawMessage `json:"result"`
	ErrorCode uint32          `json:"error_code,omitempty"`
	Error     string          `json:"error,omitempty"`
}
type database struct {
	Version          int                            `json:"version"`
	Profiles         map[string]*epb.BrowserProfile `json:"profiles"`
	Runtime          *browserruntime.Spec           `json:"runtime,omitempty"`
	State            string                         `json:"state"`
	Reason           string                         `json:"reason,omitempty"`
	BootID           string                         `json:"boot_id"`
	LastStartRequest string                         `json:"last_start_request,omitempty"`
	LastStartDigest  string                         `json:"last_start_digest,omitempty"`
	Receipts         map[string]receipt             `json:"receipts"`
	Order            []string                       `json:"order"`
}

type Worker struct {
	mu               sync.Mutex
	control          sync.Mutex
	manager          browserruntime.Manager
	core             sdk.CoreAPI
	config           config
	stateDir         string
	db               database
	persisted        database
	lock             *os.File
	lifetime         context.Context
	cancel           context.CancelFunc
	closed           bool
	active           sync.WaitGroup
	calls            int
	stopped          chan struct{}
	attachments      map[string]*attachment
	controller       string
	controlRevision  string
	fileGate         chan struct{}
	bootID           func() (string, error)
	writeJSON        func(string, any, int64) error
	observationError string
}

func New() *Worker { return NewWithManager(browserruntime.SystemManager{}) }

// NewWithManager allows file-backed lifecycle tests without touching systemd,
// Chrome, the workstation display, or the network.
func NewWithManager(manager browserruntime.Manager) *Worker {
	return &Worker{manager: manager, attachments: make(map[string]*attachment), controlRevision: browserruntime.NewID(), fileGate: make(chan struct{}, 1), bootID: linuxBootID, writeJSON: browserruntime.AtomicJSON}
}
func (worker *Worker) Info() *ppb.PluginInfo {
	return &ppb.PluginInfo{Name: "browser", Version: buildinfo.Version, Side: "server", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", "browser.remote.v1"}}}
}
func (worker *Worker) Init(_ context.Context, input sdk.InitContext) error {
	if input.Side != "server" || !filepath.IsAbs(input.StateDir) || worker.manager == nil {
		return errors.New("invalid browser server initialization")
	}
	parsed, err := parseConfig(input.Config)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.stateDir != "" {
		return errors.New("browser already initialized")
	}
	if err := browserruntime.PrivateDir(input.StateDir); err != nil {
		return err
	}
	lock, err := lockState(filepath.Join(input.StateDir, "browser.lock"))
	if err != nil {
		return err
	}
	state := database{Version: 1, Profiles: make(map[string]*epb.BrowserProfile), State: "stopped", Receipts: make(map[string]receipt)}
	if err := browserruntime.ReadJSON(filepath.Join(input.StateDir, "profiles.json"), &state, maxDatabaseBytes); err != nil && !errors.Is(err, os.ErrNotExist) {
		lock.Close()
		return err
	}
	if state.Version != 1 || state.Profiles == nil || state.Receipts == nil || len(state.Profiles) > 128 || len(state.Order) > 256 {
		lock.Close()
		return errors.New("invalid browser profile database")
	}
	bootCount := 0
	for id, profile := range state.Profiles {
		if profile == nil || profile.Id != id || !browserruntime.ValidID(id) {
			lock.Close()
			return errors.New("invalid persisted browser profile")
		}
		if profile.BootStart {
			bootCount++
		}
	}
	if bootCount > 1 {
		lock.Close()
		return errors.New("multiple persisted browser boot targets")
	}
	if state.Runtime != nil {
		if err := state.Runtime.Validate(); err != nil {
			lock.Close()
			return err
		}
		if state.Profiles[state.Runtime.ProfileID] == nil {
			lock.Close()
			return errors.New("runtime profile record missing; refusing orphaned instance")
		}
	}
	if os.Getenv("SHIKI_BROWSER_DISABLE_ONLY") != "1" {
		if err := cleanInterruptedUploads(input.StateDir, state.Profiles); err != nil {
			lock.Close()
			return err
		}
	}
	worker.persisted = cloneDatabase(state)
	worker.stateDir, worker.core, worker.config, worker.db, worker.lock = input.StateDir, input.Core, parsed, state, lock
	return nil
}
func cloneDatabase(value database) database {
	encoded, _ := json.Marshal(value)
	var copy database
	_ = json.Unmarshal(encoded, &copy)
	return copy
}
func (worker *Worker) saveLocked() error {
	err := worker.writeJSON(filepath.Join(worker.stateDir, "profiles.json"), &worker.db, maxDatabaseBytes)
	var published *browserruntime.PublishedError
	if err != nil && !errors.As(err, &published) {
		worker.db = cloneDatabase(worker.persisted)
		return err
	}
	worker.persisted = cloneDatabase(worker.db)
	return err
}
func (worker *Worker) Start(parent context.Context) error {
	worker.mu.Lock()
	if worker.stateDir == "" || worker.closed {
		worker.mu.Unlock()
		return errors.New("browser not initialized")
	}
	if worker.lifetime != nil {
		worker.mu.Unlock()
		return nil
	}
	worker.lifetime, worker.cancel = context.WithCancel(parent)
	boot, err := worker.bootID()
	if err != nil {
		worker.mu.Unlock()
		return err
	}
	newBoot := worker.db.BootID != boot
	worker.db.BootID = boot
	// Persist before launching: plugin/daemon restarts in this boot must never
	// resurrect an explicitly stopped or crashed Chrome.
	if err := worker.saveLocked(); err != nil {
		worker.mu.Unlock()
		return err
	}
	if os.Getenv("SHIKI_BROWSER_DISABLE_ONLY") != "1" {
		worker.active.Add(1)
		go worker.monitorRuntime(worker.lifetime)
	}
	var target string
	if newBoot && os.Getenv("SHIKI_BROWSER_DISABLE_ONLY") != "1" {
		for _, profile := range worker.db.Profiles {
			if profile.BootStart {
				target = profile.Id
			}
		}
	}
	current := worker.db.Runtime
	reconnect := current != nil && worker.db.State != "stopped" && os.Getenv("SHIKI_BROWSER_DISABLE_ONLY") != "1"
	worker.mu.Unlock()
	if reconnect {
		snapshot, err := worker.manager.Inspect(parent, *current)
		if err != nil {
			return err
		}
		worker.mu.Lock()
		worker.db.State, worker.db.Reason = snapshot.State, snapshot.Reason
		err = worker.saveLocked()
		worker.mu.Unlock()
		if err != nil {
			return err
		}
		if snapshot.State == "running" {
			target = ""
		}
	}
	if target != "" {
		_, err = worker.StartBrowser(parent, &epb.BrowserActionRequest{ProfileId: target, Confirm: true, RequestId: "boot-" + boot + "-" + target})
		return err
	}
	return nil
}
func (worker *Worker) Stop(ctx context.Context) error {
	worker.mu.Lock()
	if worker.stopped == nil {
		worker.closed = true
		if worker.cancel != nil {
			worker.cancel()
		}
		worker.stopped = make(chan struct{})
		go func() {
			worker.active.Wait()
			worker.mu.Lock()
			if worker.lock != nil {
				worker.lock.Close()
				worker.lock = nil
			}
			worker.mu.Unlock()
			close(worker.stopped)
		}()
	}
	done := worker.stopped
	worker.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (worker *Worker) Health(context.Context) (*pb.HealthStatus, error) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	status := pb.HealthStatus_HEALTHY
	if worker.closed || worker.lifetime == nil || worker.lifetime.Err() != nil {
		status = pb.HealthStatus_UNHEALTHY
	}
	return &pb.HealthStatus{Status: status}, nil
}
func (worker *Worker) begin(parent context.Context) (context.Context, func(), error) {
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	worker.mu.Lock()
	if err := parent.Err(); err != nil {
		worker.mu.Unlock()
		return nil, nil, err
	}
	if worker.closed || worker.lifetime == nil || worker.lifetime.Err() != nil {
		worker.mu.Unlock()
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "BROWSER_NOT_READY")
	}
	if worker.calls >= 96 {
		worker.mu.Unlock()
		return nil, nil, connect.NewError(connect.CodeResourceExhausted, "BROWSER_CALL_LIMIT")
	}
	worker.calls++
	worker.active.Add(1)
	lifetime := worker.lifetime
	worker.mu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(lifetime, cancel)
	return ctx, func() {
		stop()
		cancel()
		worker.mu.Lock()
		worker.calls--
		worker.mu.Unlock()
		worker.active.Done()
	}, nil
}
func requestDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func validRequestID(id string) bool {
	return len(id) > 0 && len(id) <= 160 && utf8.ValidString(id) && !strings.ContainsAny(id, "\x00\r\n")
}
func (worker *Worker) replayLocked(id, method string, input, result any) (bool, error) {
	if !validRequestID(id) {
		return false, connect.NewError(connect.CodeInvalidArgument, "request_id is required and bounded")
	}
	if worker.db.LastStartRequest == id && (method != "start" || worker.db.LastStartDigest != requestDigest(input)) {
		return false, connect.NewError(connect.CodeAlreadyExists, "request_id belongs to a persisted browser launch intent")
	}
	record, exists := worker.db.Receipts[id]
	if !exists {
		return false, nil
	}
	if record.Method != method || record.Digest != requestDigest(input) {
		return false, connect.NewError(connect.CodeAlreadyExists, "request_id was used for another operation")
	}
	if err := json.Unmarshal(record.Result, result); err != nil {
		return false, err
	}
	if record.ErrorCode != 0 {
		return true, connect.NewError(connect.Code(record.ErrorCode), record.Error)
	}
	return true, nil
}
func (worker *Worker) rememberLocked(id, method string, input, result any) error {
	return worker.rememberOutcomeLocked(id, method, input, result, nil)
}
func (worker *Worker) rememberOutcomeLocked(id, method string, input, result any, failure error) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if _, exists := worker.db.Receipts[id]; !exists {
		worker.db.Order = append(worker.db.Order, id)
	}
	record := receipt{Method: method, Digest: requestDigest(input), Result: encoded}
	if failure != nil {
		record.ErrorCode = uint32(connect.CodeOf(failure))
		record.Error = failure.Error()
	}
	worker.db.Receipts[id] = record
	for len(worker.db.Order) > 256 {
		delete(worker.db.Receipts, worker.db.Order[0])
		worker.db.Order = worker.db.Order[1:]
	}
	return worker.saveLocked()
}
func (worker *Worker) ListBrowserProfiles(parent context.Context, _ *epb.BrowserScope) (*epb.BrowserProfileList, error) {
	_, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	worker.mu.Lock()
	defer worker.mu.Unlock()
	result := &epb.BrowserProfileList{}
	for _, profile := range worker.db.Profiles {
		copy := proto.Clone(profile).(*epb.BrowserProfile)
		if copy.State != "deleting" {
			copy.State = "stopped"
			if worker.db.Runtime != nil && worker.db.Runtime.ProfileID == profile.Id {
				copy.State = worker.db.State
				if worker.observationError != "" && copy.State != "stopped" {
					copy.State = "unknown"
				}
			}
		}
		result.Profiles = append(result.Profiles, copy)
	}
	sort.Slice(result.Profiles, func(i, j int) bool { return result.Profiles[i].Id < result.Profiles[j].Id })
	return result, nil
}
func (worker *Worker) PutBrowserProfile(parent context.Context, request *epb.PutBrowserProfileRequest) (*epb.BrowserProfile, error) {
	ctx, done, err := worker.begin(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	if request == nil || request.Profile == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "profile is required")
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result epb.BrowserProfile
	if replay, err := worker.replayLocked(request.RequestId, "put", request, &result); replay || err != nil {
		return &result, err
	}
	profile := proto.Clone(request.Profile).(*epb.BrowserProfile)
	if profile.Id == "" {
		profile.Id = browserruntime.NewID()
	}
	if !browserruntime.ValidID(profile.Id) || !utf8.ValidString(profile.Name) || strings.TrimSpace(profile.Name) == "" || len(profile.Name) > 128 || strings.IndexFunc(profile.Name, unicode.IsControl) >= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, "invalid browser profile identity/name")
	}
	if err := validateRegion(profile.Region); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	previous := worker.db.Profiles[profile.Id]
	if previous != nil && previous.State == "deleting" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "confirmed deletion is pending; retry DeleteBrowserProfile rather than resurrecting its data")
	}
	if previous == nil {
		if request.ExpectedRevision != "" {
			return nil, connect.NewError(connect.CodeAborted, "profile does not exist at expected revision")
		}
		if len(worker.db.Profiles) >= 128 {
			return nil, connect.NewError(connect.CodeResourceExhausted, "browser profile limit")
		}
		profile.CreatedAt = time.Now().Unix()
	} else {
		if request.ExpectedRevision != previous.Revision {
			return nil, connect.NewError(connect.CodeAborted, "profile revision changed")
		}
		profile.CreatedAt = previous.CreatedAt
	}
	if profile.BootStart {
		for id, existing := range worker.db.Profiles {
			if id != profile.Id && existing.BootStart {
				if !request.ReplaceBootProfile {
					return nil, connect.NewError(connect.CodeFailedPrecondition, "explicit replacement of the current boot target is required")
				}
				updated := proto.Clone(existing).(*epb.BrowserProfile)
				updated.BootStart = false
				updated.Revision = browserruntime.NewID()
				worker.db.Profiles[id] = updated
			}
		}
	}
	profile.Revision, profile.State = browserruntime.NewID(), "stopped"
	worker.db.Profiles[profile.Id] = profile
	if err := worker.rememberLocked(request.RequestId, "put", request, profile); err != nil {
		return nil, err
	}
	return proto.Clone(profile).(*epb.BrowserProfile), nil
}

var localePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.@-]{0,63}$`)
var browserLanguagePattern = regexp.MustCompile(`^[A-Za-z]{2,3}([_-][A-Za-z]{2,8})?$`)

func validateRegion(region *epb.RegionConfig) error {
	if region == nil {
		return nil
	}
	if len(region.Timezone) > 128 {
		return errors.New("region timezone exceeds bound")
	}
	if region.Timezone == "Local" {
		return errors.New("an explicit IANA timezone is required, not the process-local alias")
	}
	if region.Dns != nil && (region.Dns.ManageSystem || len(region.Dns.Servers) > 0 || len(region.Dns.CheckDomains) > 0) {
		return errors.New("profile DNS configures Chrome only; use the region plugin for explicit system DNS changes")
	}
	if region.Timezone != "" {
		if _, err := time.LoadLocation(region.Timezone); err != nil {
			return errors.New("invalid region timezone")
		}
	}
	if region.Locale != "" && (!localePattern.MatchString(region.Locale) || !browserruntime.UTF8Locale(region.Locale)) {
		return errors.New("browser locale must be an explicit UTF-8 locale")
	}
	if region.Country != "" && region.Country != "JP" && region.Country != "US" {
		return errors.New("initial region countries are JP and US")
	}
	if region.Country != "" && (region.Timezone == "" || region.Locale == "" || len(region.Languages) == 0) {
		return errors.New("a country label is not a region configuration: explicitly specify timezone, UTF-8 locale and languages, or omit region to inherit inspected host settings")
	}
	if len(region.Languages) > 16 {
		return errors.New("too many browser languages")
	}
	for _, language := range region.Languages {
		if !browserLanguagePattern.MatchString(language) {
			return errors.New("browser languages must be supported gettext/BCP47 identifiers without unsupported @modifiers")
		}
	}
	allowed := map[string]bool{"LC_ADDRESS": true, "LC_COLLATE": true, "LC_CTYPE": true, "LC_IDENTIFICATION": true, "LC_MEASUREMENT": true, "LC_MESSAGES": true, "LC_MONETARY": true, "LC_NAME": true, "LC_NUMERIC": true, "LC_PAPER": true, "LC_TELEPHONE": true, "LC_TIME": true}
	for key, value := range region.Formats {
		if !allowed[key] || !localePattern.MatchString(value) || !browserruntime.UTF8Locale(value) {
			return errors.New("locale format categories must be individual UTF-8 locales; LC_ALL is forbidden")
		}
	}
	if region.Dns != nil {
		switch region.Dns.ChromeMode {
		case "", "unchanged", "system":
		case "secure", "automatic":
			if len(region.Dns.ChromeTemplates) == 0 {
				return errors.New("Chrome Secure DNS requires explicit templates")
			}
		default:
			return errors.New("invalid Chrome Secure DNS mode")
		}
		if len(region.Dns.ChromeTemplates) > 8 {
			return errors.New("too many Chrome DNS templates")
		}
		for _, template := range region.Dns.ChromeTemplates {
			if !strings.HasPrefix(template, "https://") || len(template) > 2048 || strings.ContainsAny(template, "\r\n\x00 ") {
				return errors.New("invalid explicit Chrome DNS template")
			}
		}
	}
	return nil
}
