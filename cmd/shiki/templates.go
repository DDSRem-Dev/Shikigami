package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/spf13/cobra"
)

type templateResult struct {
	Name      string      `json:"name"`
	Session   *pb.Session `json:"session,omitempty"`
	ErrorCode string      `json:"error_code,omitempty"`
	Error     string      `json:"error,omitempty"`
}
type templateReport struct {
	Template string           `json:"template"`
	Project  *pb.Project      `json:"project"`
	Results  []templateResult `json:"results"`
}

func (a *app) templateCommands() (*cobra.Command, *cobra.Command) {
	list := &cobra.Command{Use: "templates", Args: cobra.NoArgs, Short: "List configured session templates", RunE: func(cmd *cobra.Command, _ []string) error {
		_, cfg, e := client.LoadConfig(a.configPath)
		if e != nil {
			return e
		}
		if a.json {
			return a.printJSON(cfg.Templates)
		}
		names := make([]string, 0, len(cfg.Templates))
		for name := range cfg.Templates {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			template := cfg.Templates[name]
			if _, e = fmt.Fprintf(a.output, "%s · %d sessions · %s\n", safeText(name), len(template.Sessions), safeText(template.Description)); e != nil {
				return e
			}
		}
		return nil
	}}
	var templateName, projectName, remotePath string
	var yes bool
	initialize := &cobra.Command{Use: "init", Args: cobra.NoArgs, Short: "Create template sessions in a remote project and report each result", Long: "Create independent sessions from a configured template. Working directories are resolved inside the remote project and must already exist. auto_start controls Claude only. Every item is reported, successful items are retained if another fails, and no terminal is attached automatically.", RunE: func(cmd *cobra.Command, _ []string) error {
		if templateName == "" {
			return errors.New("init requires --template")
		}
		if a.project != "" && (projectName != "" || remotePath != "") {
			return errors.New("use --project for an existing project, or --name and --path for a new one")
		}
		if !a.interactive() {
			if !yes {
				return errors.New("init creates sessions and requires --yes in non-interactive mode")
			}
			if a.project == "" && (projectName == "" || remotePath == "") {
				return errors.New("init requires --project or both --name and --path in non-interactive mode")
			}
		}
		// Read and validate the complete template before opening IPC or mutating a project.
		_, cfg, e := client.LoadConfig(a.configPath)
		if e != nil {
			return e
		}
		template, ok := cfg.Templates[templateName]
		if !ok {
			return fmt.Errorf("template %q was not found in client configuration", templateName)
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			server, e := c.ResolveServer(cmd.Context(), a.server)
			if e != nil {
				return e
			}
			remote := c.Remote(server.Config.Id)
			var project *pb.Project
			if a.project != "" {
				project, e = c.ResolveProject(cmd.Context(), server.Config.Id, a.project)
				if e != nil {
					return e
				}
			} else {
				if projectName == "" {
					projectName, e = a.prompt(cmd.Context(), "New remote project name", "")
					if e != nil {
						return e
					}
				}
				if remotePath == "" {
					remotePath, e = a.prompt(cmd.Context(), "Absolute remote project directory", "")
					if e != nil {
						return e
					}
				}
				if !path.IsAbs(remotePath) {
					return errors.New("--path must be an absolute remote project directory")
				}
			}
			label := fmt.Sprintf("Create %d sessions from template %s using current remote project files", len(template.Sessions), safeText(templateName))
			if e = a.confirm(cmd.Context(), label, yes); e != nil {
				return e
			}
			batchID := a.id()
			if project == nil {
				project, e = remote.CreateProject(cmd.Context(), &pb.CreateProjectRequest{Name: projectName, Path: remotePath, RequestId: templateRequestID(batchID, "project")})
				if e != nil {
					return e
				}
			}
			report := templateReport{Template: templateName, Project: project, Results: make([]templateResult, 0, len(template.Sessions))}
			failed := 0
			for index, entry := range template.Sessions {
				request := templateCreateRequest(entry, project.Id, templateRequestID(batchID, fmt.Sprintf("session-%d", index)))
				result := templateResult{Name: entry.Name}
				result.Session, e = remote.CreateSession(cmd.Context(), request)
				if e != nil {
					failed++
					result.ErrorCode = connect.CodeOf(e).String()
					result.Error = e.Error()
				}
				report.Results = append(report.Results, result)
			}
			if a.json {
				if e = a.printJSON(report); e != nil {
					return e
				}
			} else {
				for _, result := range report.Results {
					if result.Error != "" {
						_, e = fmt.Fprintf(a.output, "FAILED %s · %s · %s\n", safeText(result.Name), result.ErrorCode, safeText(result.Error))
					} else {
						_, e = fmt.Fprintf(a.output, "CREATED %s · %s · %s\n", safeText(result.Name), result.Session.Id, safeText(result.Session.WorkingDir))
					}
					if e != nil {
						return e
					}
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d template sessions failed; successful sessions were retained", failed, len(report.Results))
			}
			return nil
		})
	}}
	initialize.Flags().StringVar(&templateName, "template", "", "Required configured template name")
	initialize.Flags().StringVar(&projectName, "name", "", "Name for a new remote project (use --project for an existing one)")
	initialize.Flags().StringVar(&remotePath, "path", "", "Absolute path for a new remote project; never inferred from local cwd")
	initialize.Flags().BoolVar(&yes, "yes", false, "Confirm creating all listed template sessions")
	return initialize, list
}
func templateRequestID(batch, operation string) string {
	hash := sha256.Sum256([]byte(batch + "\x00" + operation))
	return "template_" + hex.EncodeToString(hash[:])
}
func templateCreateRequest(entry config.TemplateSession, projectID, requestID string) *pb.CreateSessionRequest {
	start := entry.AutoStart == nil || *entry.AutoStart
	return &pb.CreateSessionRequest{ProjectId: projectID, Name: entry.Name, WorkingDir: entry.WorkingDir, Description: entry.Description, Tags: append([]string(nil), entry.Tags...), Metadata: config.SnapshotMetadata(entry.Metadata), AutoStartClaude: &start, RequestId: requestID}
}
