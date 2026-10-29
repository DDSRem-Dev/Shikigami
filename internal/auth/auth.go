// Package auth implements daemon-scoped device credentials and live revocation.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/golang-jwt/jwt/v5"
)

const Issuer = "shikigami"

var (
	ErrUnauthenticated  = errors.New("invalid, expired or revoked credential")
	ErrPermissionDenied = errors.New("permission denied")
	ErrRevoked          = errors.New("credential revoked")
	ErrExpired          = errors.New("credential expired")
)

type Claims struct {
	jwt.RegisteredClaims
	User        string   `json:"user"`
	Permissions []string `json:"permissions"`
}

type TokenInfo struct {
	ID          string   `json:"id"`
	User        string   `json:"user"`
	Permissions []string `json:"permissions"`
	IssuedAt    int64    `json:"issued_at"`
	ExpiresAt   int64    `json:"expires_at"`
	RevokedAt   int64    `json:"revoked_at,omitempty"`
}

type identity struct {
	DaemonID string `json:"daemon_id"`
	User     string `json:"user"`
	Secret   string `json:"secret"`
}

type activeCall struct {
	id      string
	expires time.Time
	cancel  context.CancelCauseFunc
}

type Manager struct {
	dir      string
	user     string
	mu       sync.Mutex
	calls    map[uint64]activeCall
	sequence uint64
	closed   bool
	stop     chan struct{}
	done     chan struct{}
}

// Open uses a separate auth state directory. Each operation takes a short file
// lock and reloads state, allowing local token commands while daemon is serving.
func Open(dir, user string) (*Manager, error) {
	if user == "" {
		return nil, errors.New("run user is required")
	}
	m := &Manager{dir: dir, user: user, calls: map[uint64]activeCall{}, stop: make(chan struct{}), done: make(chan struct{})}
	err := m.withStore(func(s *storage.Store) error {
		snapshot, err := s.View()
		if err != nil {
			return err
		}
		var record identity
		err = snapshot.Get("identity", "daemon", &record)
		if err == nil {
			return validateIdentity(record, user)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		secret := make([]byte, 32)
		if _, err = rand.Read(secret); err != nil {
			return err
		}
		id, err := randomID("daemon_")
		if err != nil {
			return err
		}
		record = identity{DaemonID: id, User: user, Secret: base64.RawStdEncoding.EncodeToString(secret)}
		return s.Update(func(snapshot *storage.Snapshot) error { return snapshot.Put("identity", "daemon", record) })
	})
	if err != nil {
		return nil, err
	}
	go m.monitor()
	return m, nil
}

func validateIdentity(record identity, user string) error {
	secret, err := base64.RawStdEncoding.DecodeString(record.Secret)
	if err != nil || len(secret) != 32 || !strings.HasPrefix(record.DaemonID, "daemon_") || record.User != user {
		return errors.New("auth identity does not match configured user or is corrupt")
	}
	return nil
}

func (m *Manager) withStore(fn func(*storage.Store) error) error {
	s, err := storage.Open(m.dir, storage.Options{DisableRecovery: true, LockTimeout: 5 * time.Second, Validate: func(snapshot *storage.Snapshot) error {
		var id identity
		if err := snapshot.Get("identity", "daemon", &id); err != nil {
			if snapshot.Revision == 0 && errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return errors.New("missing daemon auth identity")
		}
		if err := validateIdentity(id, m.user); err != nil {
			return err
		}
		for key := range snapshot.Buckets["tokens"] {
			var token TokenInfo
			if err := snapshot.Get("tokens", key, &token); err != nil {
				return err
			}
			if token.ID != key || token.User != m.user || token.IssuedAt <= 0 || token.ExpiresAt <= token.IssuedAt {
				return errors.New("invalid token registry")
			}
		}
		return nil
	}})
	if err != nil {
		return err
	}
	err = fn(s)
	return errors.Join(err, s.Close())
}

func (m *Manager) DaemonID() (id string, err error) {
	err = m.withStore(func(s *storage.Store) error {
		state, err := s.View()
		if err != nil {
			return err
		}
		var record identity
		if err = state.Get("identity", "daemon", &record); err != nil {
			return err
		}
		id = record.DaemonID
		return nil
	})
	return
}

// Create returns the secret token exactly once; the registry stores only claims.
func (m *Manager) Create(user string, permissions []string, lifetime time.Duration) (token string, info TokenInfo, err error) {
	if user != m.user {
		return "", info, ErrPermissionDenied
	}
	permissions, err = normalizePermissions(permissions)
	if err != nil {
		return "", info, err
	}
	if lifetime < time.Second {
		return "", info, errors.New("token lifetime must be at least one second")
	}
	err = m.withStore(func(s *storage.Store) error {
		return s.Update(func(state *storage.Snapshot) error {
			var record identity
			if err := state.Get("identity", "daemon", &record); err != nil {
				return err
			}
			var err error
			token, info, err = issue(record, permissions, lifetime)
			if err != nil {
				return err
			}
			return state.Put("tokens", info.ID, info)
		})
	})
	if err != nil {
		token = ""
	}
	return
}

func issue(record identity, permissions []string, lifetime time.Duration) (string, TokenInfo, error) {
	id, err := randomID("tok_")
	if err != nil {
		return "", TokenInfo{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	info := TokenInfo{ID: id, User: record.User, Permissions: append([]string{}, permissions...), IssuedAt: now.Unix(), ExpiresAt: now.Add(lifetime).Unix()}
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Audience: jwt.ClaimStrings{record.DaemonID}, ID: id, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(time.Unix(info.ExpiresAt, 0))}, User: record.User, Permissions: info.Permissions}
	secret, err := base64.RawStdEncoding.DecodeString(record.Secret)
	if err != nil {
		return "", info, err
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		return "", info, err
	}
	return "shiki_" + token, info, nil
}

func (m *Manager) List() (result []TokenInfo, err error) {
	err = m.withStore(func(s *storage.Store) error {
		state, err := s.View()
		if err != nil {
			return err
		}
		for id := range state.Buckets["tokens"] {
			var info TokenInfo
			if err = state.Get("tokens", id, &info); err != nil {
				return err
			}
			result = append(result, info)
		}
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return
}

func (m *Manager) Revoke(id string) error {
	err := m.withStore(func(s *storage.Store) error {
		return s.Update(func(state *storage.Snapshot) error {
			var info TokenInfo
			if err := state.Get("tokens", id, &info); err != nil {
				return err
			}
			if info.RevokedAt == 0 {
				info.RevokedAt = time.Now().Unix()
			}
			return state.Put("tokens", id, info)
		})
	})
	if err == nil {
		m.cancelToken(id, ErrRevoked)
	}
	return err
}

// Refresh atomically revokes the old token and stores the newly signed claims.
// This is a local administrative operation, not a bearer-authenticated renewal.
func (m *Manager) Refresh(id string, lifetime time.Duration) (token string, info TokenInfo, err error) {
	if lifetime < time.Second {
		return "", info, errors.New("token lifetime must be at least one second")
	}
	err = m.withStore(func(s *storage.Store) error {
		return s.Update(func(state *storage.Snapshot) error {
			var old TokenInfo
			if err := state.Get("tokens", id, &old); err != nil {
				return err
			}
			if old.RevokedAt != 0 {
				return ErrRevoked
			}
			var record identity
			if err := state.Get("identity", "daemon", &record); err != nil {
				return err
			}
			var err error
			token, info, err = issue(record, old.Permissions, lifetime)
			if err != nil {
				return err
			}
			old.RevokedAt = time.Now().Unix()
			if err = state.Put("tokens", id, old); err != nil {
				return err
			}
			return state.Put("tokens", info.ID, info)
		})
	})
	if err != nil {
		token = ""
	} else {
		m.cancelToken(id, ErrRevoked)
	}
	return
}

func (m *Manager) Verify(raw string) (*Claims, error) {
	if !strings.HasPrefix(raw, "shiki_") || len(raw) > 16<<10 {
		return nil, ErrUnauthenticated
	}
	var claims *Claims
	err := m.withStore(func(s *storage.Store) error {
		state, err := s.View()
		if err != nil {
			return err
		}
		var record identity
		if err = state.Get("identity", "daemon", &record); err != nil {
			return err
		}
		secret, err := base64.RawStdEncoding.DecodeString(record.Secret)
		if err != nil {
			return err
		}
		parsed := new(Claims)
		token, err := jwt.ParseWithClaims(strings.TrimPrefix(raw, "shiki_"), parsed, func(t *jwt.Token) (any, error) { return secret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(Issuer), jwt.WithAudience(record.DaemonID), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithStrictDecoding())
		if err != nil || !token.Valid || parsed.ID == "" || parsed.IssuedAt == nil || parsed.User != m.user {
			return ErrUnauthenticated
		}
		var info TokenInfo
		if err = state.Get("tokens", parsed.ID, &info); err != nil {
			return ErrUnauthenticated
		}
		if info.RevokedAt != 0 || info.IssuedAt != parsed.IssuedAt.Unix() || info.ExpiresAt != parsed.ExpiresAt.Unix() || !sameStrings(info.Permissions, parsed.Permissions) {
			return ErrUnauthenticated
		}
		claims = parsed
		return nil
	})
	if err != nil {
		return nil, ErrUnauthenticated
	}
	return claims, nil
}

// BindContext verifies and registers a live call before returning. Call release
// exactly once on RPC completion. Expiry and local/cross-process revocation
// cancel the context; only the affected call is canceled when its parent ends.
func (m *Manager) BindContext(parent context.Context, raw string) (context.Context, *Claims, func(), error) {
	claims, err := m.Verify(raw)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithCancelCause(parent)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel(ErrUnauthenticated)
		return nil, nil, nil, ErrUnauthenticated
	}
	m.sequence++
	key := m.sequence
	m.calls[key] = activeCall{id: claims.ID, expires: claims.ExpiresAt.Time, cancel: cancel}
	m.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { m.mu.Lock(); delete(m.calls, key); m.mu.Unlock(); cancel(context.Canceled) }) }
	// Recheck after registration to close the verify/register revocation race.
	if _, err = m.Verify(raw); err != nil {
		release()
		return nil, nil, nil, err
	}
	stop := context.AfterFunc(ctx, release)
	timer := time.AfterFunc(time.Until(claims.ExpiresAt.Time), func() { cancel(ErrExpired) })
	return context.WithValue(ctx, claimsKey{}, claims), claims, func() { stop(); timer.Stop(); release() }, nil
}

type claimsKey struct{}

func FromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(*Claims)
	return claims, ok
}

func (m *Manager) cancelToken(id string, cause error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, call := range m.calls {
		if call.id == id {
			call.cancel(cause)
		}
	}
}

func (m *Manager) monitor() {
	defer close(m.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.mu.Lock()
			active := len(m.calls) > 0
			m.mu.Unlock()
			if !active {
				continue
			}
			tokens, err := m.List()
			byID := map[string]TokenInfo{}
			for _, token := range tokens {
				byID[token.ID] = token
			}
			m.mu.Lock()
			for _, call := range m.calls {
				token, ok := byID[call.id]
				if err != nil || !ok || token.RevokedAt != 0 {
					call.cancel(ErrRevoked)
				} else if !time.Now().Before(call.expires) {
					call.cancel(ErrExpired)
				}
			}
			m.mu.Unlock()
		}
	}
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		close(m.stop)
		for _, call := range m.calls {
			call.cancel(context.Canceled)
		}
	}
	m.mu.Unlock()
	<-m.done
	return nil
}

func randomID(prefix string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(bytes), nil
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizePermissions(values []string) ([]string, error) {
	seen := map[string]bool{}
	var result []string
	for _, permission := range values {
		if permission != "*" {
			if strings.HasSuffix(permission, ".*") {
				namespace := strings.TrimSuffix(permission, ".*")
				if !permissionNamespaces[namespace] {
					return nil, fmt.Errorf("unknown permission namespace %q", namespace)
				}
			} else if !knownPermissions[permission] {
				return nil, fmt.Errorf("unknown permission %q", permission)
			}
		}
		if !seen[permission] {
			seen[permission] = true
			result = append(result, permission)
		}
	}
	sort.Strings(result)
	return result, nil
}
