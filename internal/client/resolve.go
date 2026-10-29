package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

func (c *Client) ResolveServer(ctx context.Context, name string) (*apb.ServerRecord, error) {
	list, e := c.agent.ListServers(ctx, &pb.Empty{})
	if e != nil {
		return nil, e
	}
	if name == "" {
		preferences, e := c.agent.GetPreferences(ctx, &pb.Empty{})
		if e != nil {
			return nil, e
		}
		name = preferences.DefaultServerId
		if name == "" && len(list.Servers) == 1 {
			return list.Servers[0], nil
		}
	}
	var matches []*apb.ServerRecord
	for _, server := range list.Servers {
		if server.Config != nil && (server.Config.Id == name || server.Config.Name == name) {
			matches = append(matches, server)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, errors.New("server name is ambiguous; use its ID")
	}
	if name == "" {
		return nil, errors.New("no default server; use --server or shiki servers switch")
	}
	return nil, fmt.Errorf("server %q was not found", name)
}
func (c *Client) ResolveProject(ctx context.Context, serverID, name string) (*pb.Project, error) {
	if name == "" {
		return nil, errors.New("--project is required")
	}
	if strings.HasPrefix(name, "proj_") {
		return c.Remote(serverID).GetProject(ctx, &pb.GetProjectRequest{Id: name})
	}
	list, e := c.Remote(serverID).ListProjects(ctx, &pb.ListProjectsRequest{})
	if e != nil {
		return nil, e
	}
	var result *pb.Project
	for _, project := range list.Projects {
		if project.Name == name {
			if result != nil {
				return nil, errors.New("project name is ambiguous; use its ID")
			}
			result = project
		}
	}
	if result == nil {
		return nil, connect.NewError(connect.CodeNotFound, "project was not found")
	}
	return result, nil
}
func (c *Client) ResolveSession(ctx context.Context, serverID, projectID, name string) (*pb.Session, error) {
	if name == "" {
		return nil, errors.New("session ID or name is required")
	}
	if strings.HasPrefix(name, "sess_") {
		result, e := c.Remote(serverID).GetSession(ctx, &pb.GetSessionRequest{Id: name})
		if e == nil && projectID != "" && result.ProjectId != projectID {
			return nil, errors.New("session does not belong to --project")
		}
		return result, e
	}
	sessions, e := c.Remote(serverID).ListSessions(ctx, &pb.ListSessionsRequest{})
	if e != nil {
		return nil, e
	}
	var result *pb.Session
	for _, session := range sessions.Sessions {
		if session.Name == name && (projectID == "" || session.ProjectId == projectID) {
			if result != nil {
				return nil, errors.New("session name is ambiguous across projects; use --project or a session ID")
			}
			result = session
		}
	}
	if result == nil {
		return nil, connect.NewError(connect.CodeNotFound, "session was not found")
	}
	return result, nil
}

// MappingForDirectory selects the most specific canonical directory mapping.
// It compares path components, so /project-old never matches /project.
func MappingForDirectory(cwd string, mappings []*ppb.Mapping) (*ppb.Mapping, string, error) {
	cwd, e := filepath.EvalSymlinks(cwd)
	if e != nil {
		return nil, "", e
	}
	cwd, e = filepath.Abs(cwd)
	if e != nil {
		return nil, "", e
	}
	var found *ppb.Mapping
	var relative string
	bestLength := -1
	for _, mapping := range mappings {
		root, e := filepath.EvalSymlinks(mapping.LocalRoot)
		if e != nil {
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			return nil, "", e
		}
		rel, e := filepath.Rel(root, cwd)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if len(root) > bestLength {
			bestLength = len(root)
			found = mapping
			relative = rel
		}
	}
	return found, relative, nil
}
