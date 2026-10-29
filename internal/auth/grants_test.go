package auth

import (
	"slices"
	"strings"
	"testing"
)

func TestNegotiationGrantsExpandOnlyKnownEffectivePermissions(t *testing.T) {
	for _, permissions := range [][]string{{"*"}, {"session.*", "session.attach", "unknown.*"}, {"system.stats", "system.stats", "unknown.name"}} {
		claims := &Claims{Permissions: permissions}
		grants := claims.GrantedPermissions()
		if !slices.IsSorted(grants) {
			t.Fatal("grants are not stable", grants)
		}
		for i, permission := range grants {
			if !knownPermissions[permission] || strings.Contains(permission, "*") || !claims.HasPermission(permission) || i > 0 && grants[i-1] == permission {
				t.Fatal("invalid effective grant", permission)
			}
		}
		for permission := range knownPermissions {
			if claims.HasPermission(permission) != slices.Contains(grants, permission) {
				t.Fatal("effective grant omitted", permission)
			}
		}
	}
	var missing *Claims
	if len(missing.GrantedPermissions()) != 0 {
		t.Fatal("missing claims granted permissions")
	}
}
