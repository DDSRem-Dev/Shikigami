package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
)

const frontendIdleTimeout = 10 * time.Minute
const maxFrontends = 256

type identityKey struct{}
type Identity struct{ FrontendID, ServerID string }

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(identityKey{}).(Identity)
	return v, ok
}

type attachmentKey struct{ server, session, attachment string }
type frontend struct {
	capabilities []string
	ctx          context.Context
	cancel       context.CancelFunc
	lastCall     time.Time
	active       int
}

// Registry owns frontend lifetimes. Background sync/forward tasks must use the
// agent's root context, never one of the frontend contexts managed here.
type Registry struct {
	mu          sync.Mutex
	instance    string
	root        context.Context
	frontends   map[string]*frontend
	attachments map[attachmentKey]string
}

func newID(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(raw[:])
}
func NewRegistry(ctx context.Context) *Registry {
	return &Registry{root: ctx, instance: newID("agent_"), frontends: map[string]*frontend{}, attachments: map[attachmentKey]string{}}
}
func (r *Registry) InstanceID() string        { return r.instance }
func (r *Registry) Register() (string, error) { return r.registerCapabilities(nil) }
func (r *Registry) registerCapabilities(capabilities []string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked(time.Now())
	if r.root.Err() != nil {
		return "", connect.NewError(connect.CodeUnavailable, "AGENT_STOPPING")
	}
	if len(r.frontends) >= maxFrontends {
		return "", connect.NewError(connect.CodeResourceExhausted, "FRONTEND_LIMIT")
	}
	id := newID("front_")
	ctx, cancel := context.WithCancel(r.root)
	r.frontends[id] = &frontend{ctx: ctx, cancel: cancel, lastCall: time.Now(), capabilities: slices.Clone(capabilities)}
	return id, nil
}
func (r *Registry) sweepLocked(now time.Time) {
	for id, f := range r.frontends {
		if f.active == 0 && now.Sub(f.lastCall) >= frontendIdleTimeout {
			f.cancel()
			delete(r.frontends, id)
		}
	}
}
func (r *Registry) bind(ctx context.Context, instance, id string, track bool) (context.Context, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if instance != r.instance {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "AGENT_RESTARTED")
	}
	r.sweepLocked(time.Now())
	f := r.frontends[id]
	if f == nil {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "HANDSHAKE_REQUIRED")
	}
	f.lastCall = time.Now()
	if !track {
		return ctx, func() {}, nil
	}
	f.active++
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(f.ctx, cancel)
	return child, func() {
		stop()
		cancel()
		r.mu.Lock()
		f.active--
		f.lastCall = time.Now()
		r.mu.Unlock()
	}, nil
}
func (r *Registry) Release(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.frontends[id]; f != nil {
		f.cancel()
		delete(r.frontends, id)
	}
	for key, owner := range r.attachments {
		if owner == id {
			delete(r.attachments, key)
		}
	}
}
func (r *Registry) AddAttachment(ctx context.Context, session, attachment string) (func(), error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok || identity.ServerID == "" || session == "" || attachment == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "ATTACHMENT_ID_REQUIRED")
	}
	key := attachmentKey{identity.ServerID, session, attachment}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frontends[identity.FrontendID] == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "HANDSHAKE_REQUIRED")
	}
	if _, exists := r.attachments[key]; exists {
		return nil, connect.NewError(connect.CodeAlreadyExists, "ATTACHMENT_EXISTS")
	}
	r.attachments[key] = identity.FrontendID
	return func() { r.mu.Lock(); delete(r.attachments, key); r.mu.Unlock() }, nil
}
func (r *Registry) OwnsAttachment(ctx context.Context, session, attachment string) bool {
	identity, ok := IdentityFromContext(ctx)
	if !ok {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	owner, exists := r.attachments[attachmentKey{identity.ServerID, session, attachment}]
	return exists && owner == identity.FrontendID
}
func (r *Registry) TerminalCount() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.attachments) }

func (r *Registry) ServerTerminalCount(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for key := range r.attachments {
		if key.server == id {
			count++
		}
	}
	return count
}

func (r *Registry) hasCapability(id, capability string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.frontends[id]
	return f != nil && slices.Contains(f.capabilities, capability)
}
