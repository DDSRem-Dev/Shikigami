package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/golang-jwt/jwt/v5"
)

func openManager(t *testing.T, dir string) *Manager {
	t.Helper()
	m, err := Open(dir, "test-user")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func createToken(t *testing.T, m *Manager, permissions ...string) (string, TokenInfo) {
	t.Helper()
	token, info, err := m.Create("test-user", permissions, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return token, info
}

func TestPersistentIdentityAndTokenRevocationAcrossManagers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "auth")
	daemon := openManager(t, dir)
	token, info := createToken(t, daemon, "session.attach")
	id, err := daemon.DaemonID()
	if err != nil {
		t.Fatal(err)
	}
	admin := openManager(t, dir)
	id2, _ := admin.DaemonID()
	if id != id2 {
		t.Fatal("daemon identity changed")
	}
	ctx, claims, release, err := daemon.BindContext(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if claims.ID != info.ID {
		t.Fatal("wrong identity")
	}
	if err = admin.Revoke(info.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("cross-process revocation did not cancel stream")
	}
	if _, err = daemon.Verify(token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("revoked token accepted")
	}
	if _, err = Open(dir, "different-user"); err == nil {
		t.Fatal("OS user switching accepted")
	}
}

func TestRefreshIsAtomicAndExpiryCancels(t *testing.T) {
	m := openManager(t, filepath.Join(t.TempDir(), "auth"))
	old, info := createToken(t, m, "system.info")
	ctx, _, release, err := m.BindContext(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// JWT/registry timestamps use whole seconds. A one-second lifetime may
	// leave almost no usable time for committing and checking the old token.
	// Allow setup time, but measure cancellation against the actual signed exp.
	newToken, newInfo, err := m.Refresh(info.ID, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if newInfo.ID == info.ID {
		t.Fatal("refresh reused jti")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("refresh did not cancel old call")
	}
	if !errors.Is(context.Cause(ctx), ErrRevoked) {
		t.Fatalf("unexpected old call cause: %v", context.Cause(ctx))
	}
	if _, err = m.Verify(old); err == nil {
		t.Fatal("old token still valid")
	}
	ctx2, claims, release2, err := m.BindContext(context.Background(), newToken)
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	expires := time.Unix(newInfo.ExpiresAt, 0)
	if !claims.ExpiresAt.Time.Equal(expires) {
		t.Fatal("JWT and registry expiry differ")
	}
	if err := ctx2.Err(); err != nil {
		t.Fatalf("new call already canceled: %v", context.Cause(ctx2))
	}
	timer := time.NewTimer(time.Until(expires) + time.Second)
	defer timer.Stop()
	select {
	case <-ctx2.Done():
		if time.Now().Before(expires) {
			t.Fatal("call canceled before signed expiry")
		}
	case <-timer.C:
		t.Fatal("expiry did not cancel")
	}
	if !errors.Is(context.Cause(ctx2), ErrExpired) {
		t.Fatalf("unexpected cause: %v", context.Cause(ctx2))
	}
	if _, err := m.Verify(newToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired token accepted: %v", err)
	}
}

func TestJWTRequiredClaimsAndAlgorithm(t *testing.T) {
	m := openManager(t, filepath.Join(t.TempDir(), "auth"))
	raw, _ := createToken(t, m, "*")
	valid, err := m.Verify(raw)
	if err != nil {
		t.Fatal(err)
	}
	var record identity
	err = m.withStore(func(s *storage.Store) error {
		state, err := s.View()
		if err != nil {
			return err
		}
		return state.Get("identity", "daemon", &record)
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := base64.RawStdEncoding.DecodeString(record.Secret)
	tests := map[string]func(*Claims){"missing-exp": func(c *Claims) { c.ExpiresAt = nil }, "missing-iat": func(c *Claims) { c.IssuedAt = nil }, "future-iat": func(c *Claims) { c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, "wrong-audience": func(c *Claims) { c.Audience = jwt.ClaimStrings{"another-daemon"} }, "wrong-issuer": func(c *Claims) { c.Issuer = "other" }, "unknown-jti": func(c *Claims) { c.ID = "unknown" }, "wrong-user": func(c *Claims) { c.User = "root" }, "changed-permissions": func(c *Claims) { c.Permissions = []string{"file.read"} }}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			copy := *valid
			mutate(&copy)
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, copy).SignedString(secret)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = m.Verify("shiki_" + signed); err == nil {
				t.Fatal("invalid claims accepted")
			}
		})
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS384, valid).SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Verify("shiki_" + signed); err == nil {
		t.Fatal("HS384 accepted")
	}
}

func TestPermissionTableCoversEveryProtocolMethod(t *testing.T) {
	service := protocol.File_shiki_proto.Services().ByName("ShikiDaemon")
	if len(methodPermissions) != service.Methods().Len() {
		t.Fatal("permission table/proto drift")
	}
	for i := 0; i < service.Methods().Len(); i++ {
		name := string(service.Methods().Get(i).Name())
		if _, ok := methodPermissions[name]; !ok {
			t.Fatalf("missing explicit permission for %s", name)
		}
	}
	admin := &Claims{Permissions: []string{"*"}}
	if err := Authorize(admin, "/shiki.v1.ShikiDaemon/Unknown"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("unknown method authorized")
	}
	observer := &Claims{Permissions: []string{"session.attach", "session.control"}}
	if err := Authorize(observer, "/shiki.v1.ShikiDaemon/TakeControl"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("control allowed without write")
	}
	if err := Authorize(observer, "/shiki.v1.ShikiDaemon/TerminalStream"); err != nil {
		t.Fatal(err)
	}
	for _, headers := range [][]string{nil, {"Bearer x"}, {"Bearer shiki_a", "Bearer shiki_b"}, {"Bearer shiki_a, Bearer shiki_b"}, {"Bearer shiki_a "}} {
		if _, err := bearer(headers); err == nil {
			t.Fatalf("ambiguous authorization accepted: %#v", headers)
		}
	}
}

func TestCorruptRegistryCannotUndoRevocation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "auth")
	m := openManager(t, dir)
	token, info := createToken(t, m, "*")
	if err := m.Revoke(info.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Verify(token); err == nil {
		t.Fatal("backup restored revoked credential")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if string(data) != "truncated" {
		t.Fatal("corrupt auth registry modified")
	}
}
