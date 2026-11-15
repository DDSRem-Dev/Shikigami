package server

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/session"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

// sessionEnvironment reads the ready region service rather than importing its
// business implementation or reading private plugin state. Pending desired
// configuration must never masquerade as the host's observed actual settings.
func (s *Service) sessionEnvironment(ctx context.Context) (*session.LaunchEnvironment, error) {
	if s.Supervisor == nil {
		return nil, nil
	}
	plugins, err := s.Supervisor.List()
	if err != nil {
		return nil, err
	}
	enabled := false
	for _, status := range plugins.Plugins {
		if status.PluginId == "region" {
			enabled = status.Enabled
			break
		}
	}
	if !enabled {
		// Region is independently optional; preserve baseline launch semantics
		// when it is not enabled instead of fabricating a region inspection.
		return nil, nil
	}
	query, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	status, err := s.DaemonDataPlane.InspectRegion(query, &epb.RegionInspectRequest{})
	if err != nil {
		// In particular do not kill an existing Claude on an explicit restart
		// when the enabled region worker cannot provide a truthful snapshot.
		return nil, err
	}
	if status == nil || status.Actual == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "REGION_ACTUAL_ENVIRONMENT_UNAVAILABLE")
	}
	actual := status.Actual
	values := make(map[string]string, 15)
	if actual.Timezone != "" {
		values["TZ"] = actual.Timezone
	}
	if actual.Locale != "" {
		values["LANG"] = actual.Locale
	}
	if len(actual.Languages) != 0 {
		values["LANGUAGE"] = strings.Join(actual.Languages, ":")
	}
	for name, value := range actual.Formats {
		switch name {
		case "LC_CTYPE", "LC_NUMERIC", "LC_TIME", "LC_COLLATE", "LC_MONETARY", "LC_MESSAGES", "LC_PAPER", "LC_NAME", "LC_ADDRESS", "LC_TELEPHONE", "LC_MEASUREMENT", "LC_IDENTIFICATION":
			if value != "" {
				values[name] = value
			}
		default:
			return nil, connect.NewError(connect.CodeDataLoss, "REGION_ENVIRONMENT_CATEGORY_INVALID")
		}
	}
	return &session.LaunchEnvironment{Revision: status.Revision, Values: values}, nil
}
