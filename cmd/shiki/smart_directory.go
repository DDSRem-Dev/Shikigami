package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func (a *app) chooseWorkingDirectory(ctx context.Context, c *client.Client, server, project string, agreement *pb.ProtocolAgreement) (string, error) {
	if !smartGrant(agreement, "file.read") || !slices.Contains(agreement.GetCapabilities(), "project.directories.v1") {
		return a.prompt(ctx, "Working directory within this project (relative path or project subdirectory)", ".")
	}
	current, token := "", ""
	seen := map[string]bool{}
	for {
		page, err := c.Remote(server).ListProjectDirectories(ctx, &pb.ListProjectDirectoriesRequest{ProjectId: project, Path: current, PageSize: 50, PageToken: token})
		if err != nil {
			return "", err
		}
		if page.ProjectId != project || page.Path != current {
			return "", errors.New("directory listing returned a different project or path")
		}
		display := current
		if display == "" {
			display = ". (project root)"
		}
		choices := []smartSessionChoice{{"use", "Use " + display}}
		for _, directory := range page.Directories {
			parent := path.Dir(directory)
			if parent == "." {
				parent = ""
			}
			if directory == "" || path.IsAbs(directory) || path.Clean(directory) != directory || parent != current || directory == ".." || strings.HasPrefix(directory, "../") {
				return "", errors.New("directory listing returned a path outside the selected scope")
			}
			choices = append(choices, smartSessionChoice{"directory:" + directory, "Browse " + directory})
		}
		if current != "" {
			choices = append(choices, smartSessionChoice{"parent", "Browse parent directory"})
		}
		if page.NextPageToken != "" {
			choices = append(choices, smartSessionChoice{"next", "Next page of directories"})
		}
		choices = append(choices, smartSessionChoice{"custom", "Enter a custom path within this project"}, smartSessionChoice{"cancel", "Cancel session creation"})
		labels := make([]string, len(choices))
		for i := range choices {
			labels[i] = choices[i].label
		}
		selected, err := a.choose(ctx, fmt.Sprintf("Working directory · server %s · project %s", safeText(server), safeText(project)), labels)
		if err != nil {
			return "", err
		}
		switch choice := choices[selected].action; choice {
		case "use":
			if current == "" {
				return ".", nil
			}
			return current, nil
		case "custom":
			return a.prompt(ctx, "Custom working directory within this project", ".")
		case "cancel":
			return "", errors.New("session creation canceled")
		case "parent":
			current = path.Dir(current)
			if current == "." {
				current = ""
			}
			token = ""
			seen = map[string]bool{}
		case "next":
			if seen[page.NextPageToken] || page.NextPageToken == token {
				return "", errors.New("directory listing repeated a pagination token")
			}
			seen[page.NextPageToken] = true
			token = page.NextPageToken
		default:
			current = strings.TrimPrefix(choice, "directory:")
			token = ""
			seen = map[string]bool{}
		}
	}
}
