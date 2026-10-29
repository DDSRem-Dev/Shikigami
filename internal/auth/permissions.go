package auth

import (
	"sort"
	"strings"
)

// Explicit mappings are deliberately independent of generated service names:
// adding a protocol RPC does not silently authorize it.
var methodPermissions = map[string][]string{
	"NegotiateProtocol": {},
	"ListPlugins":       {"plugin.manage"}, "ManagePlugin": {"plugin.manage"}, "WatchPluginLogs": {"plugin.manage"},
	"GetStats": {"system.stats"}, "WatchStats": {"system.stats"},
	"ListProjects": {"project.list"}, "GetProject": {"project.list"}, "CreateProject": {"project.create"}, "DeleteProject": {"project.delete"},
	"ListProjectSessions": {"session.list"}, "ListSessions": {"session.list"}, "GetSession": {"session.list"},
	"CreateSession": {"session.create"}, "UpdateSession": {"session.update"}, "DeleteSession": {"session.delete"},
	"GetSessionLogs": {"session.attach"}, "GetSessionScreen": {"session.attach"}, "TerminalStream": {"session.attach"}, "TakeControl": {"session.control", "session.write"}, "StopClaude": {"session.control"}, "RestartClaude": {"session.control"},
	"ListProjectDirectories": {"file.read"}, "ReadFile": {"file.read"}, "ListFiles": {"file.read"}, "StatFile": {"file.read"}, "WriteFile": {"file.write"}, "DeleteFile": {"file.write"}, "WatchFiles": {"file.watch"},
	"ListPorts": {"port.list"}, "WatchPorts": {"port.list"}, "ForwardPort": {"port.forward"}, "CheckPort": {"port.forward"},
	"GetSystemInfo": {"system.info"}, "GetClaudeVersion": {"system.info"}, "HealthCheck": {"system.info"},
	"CreateSnapshot": {"session.create"}, "RestoreSnapshot": {"session.create"}, "ListSnapshots": {"session.list"}, "DeleteSnapshot": {"session.delete"},
}

var permissionNamespaces = map[string]bool{"project": true, "session": true, "file": true, "port": true, "system": true, "plugin": true}
var knownPermissions = func() map[string]bool {
	result := map[string]bool{}
	for _, permissions := range methodPermissions {
		for _, permission := range permissions {
			result[permission] = true
		}
	}
	return result
}()

func (c *Claims) HasPermission(permission string) bool {
	if !knownPermissions[permission] {
		return false
	}
	for _, granted := range c.Permissions {
		if granted == "*" || granted == permission {
			return true
		}
		if strings.HasSuffix(granted, ".*") && strings.SplitN(permission, ".", 2)[0] == strings.TrimSuffix(granted, ".*") {
			return true
		}
	}
	return false
}

// GrantedPermissions expands only known authorization names, never JWT claims,
// account identity, unknown namespaces or wildcard patterns.
func (c *Claims) GrantedPermissions() []string {
	if c == nil {
		return nil
	}
	var result []string
	for permission := range knownPermissions {
		if c.HasPermission(permission) {
			result = append(result, permission)
		}
	}
	sort.Strings(result)
	return result
}

// Authorize requires a fully-qualified daemon procedure; unknown procedures
// are denied even for administrator credentials.
func Authorize(claims *Claims, procedure string) error {
	if claims == nil {
		return ErrUnauthenticated
	}
	const prefix = "/shiki.v1.ShikiDaemon/"
	if !strings.HasPrefix(procedure, prefix) {
		return ErrPermissionDenied
	}
	permissions, ok := methodPermissions[strings.TrimPrefix(procedure, prefix)]
	if !ok {
		return ErrPermissionDenied
	}
	for _, permission := range permissions {
		if !claims.HasPermission(permission) {
			return ErrPermissionDenied
		}
	}
	return nil
}
