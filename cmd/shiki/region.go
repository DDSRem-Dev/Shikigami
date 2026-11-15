package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func (a *app) regionCommands() *cobra.Command {
	root := &cobra.Command{Use: "region", Short: "Inspect and explicitly plan changes to a remote host's regional environment", Args: cobra.NoArgs}
	inspect := &cobra.Command{Use: "inspect", Args: cobra.NoArgs, Short: "Show actual settings, revision, pending operation and recovery deadline", Long: "Inspect actual/desired settings, helper availability, warnings, pending_operation_id, and confirm_before. After an apply timeout or lost response, recover the operation ID here, then inspect it with region operation. A client disconnect does not cancel the independent DNS recovery guard; confirm only after reviewing successful fresh DNS/connection checks.", RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withExtensionServer(cmd.Context(), func(c *client.Client, serverID string) error {
			result, err := c.Region(serverID).InspectRegion(cmd.Context(), &epb.RegionInspectRequest{ServerId: serverID})
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	root.AddCommand(inspect)

	var configFile, planRevision string
	plan := &cobra.Command{Use: "plan", Args: cobra.NoArgs, Short: "Review a bounded, versioned plan from explicit RegionConfig JSON; do not apply it", Long: "Plan whole-host timezone, UTF-8 locale, language, formats, and explicitly requested DNS changes. --file contains a RegionConfig JSON object, for example {\"country\":\"US\",\"timezone\":\"America/New_York\",\"locale\":\"en_US.UTF-8\",\"languages\":[\"en_US\"]}. US timezone and language are never guessed. DNS providers are never selected by country. Applying a country is not proof of location or service eligibility.", RunE: func(cmd *cobra.Command, _ []string) error {
		config, err := readRegionConfig(configFile)
		if err != nil {
			return err
		}
		return a.withExtensionServer(cmd.Context(), func(c *client.Client, serverID string) error {
			expected := planRevision
			if expected == "" {
				status, err := c.Region(serverID).InspectRegion(cmd.Context(), &epb.RegionInspectRequest{ServerId: serverID})
				if err != nil {
					return err
				}
				expected = status.GetRevision()
				if expected == "" {
					return errors.New("region inspect returned no revision; a safe plan cannot be created")
				}
			}
			result, err := c.Region(serverID).PlanRegion(cmd.Context(), &epb.RegionPlanRequest{ServerId: serverID, Config: config, ExpectedRevision: expected, RequestId: a.id()})
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	plan.Flags().StringVar(&configFile, "file", "", "Required: complete RegionConfig JSON; review returned changes before applying")
	plan.Flags().StringVar(&planRevision, "expected-revision", "", "Require this observed host revision; defaults to a fresh inspect")
	root.AddCommand(plan)

	var applyRevision string
	var applyYes bool
	apply := &cobra.Command{Use: "apply <plan-id>", Args: cobra.ExactArgs(1), Short: "Apply a reviewed plan; DNS changes remain subject to their recovery deadline", RunE: func(cmd *cobra.Command, args []string) error {
		if err := requiredExtensionValue("plan ID", args[0]); err != nil {
			return err
		}
		if err := requiredExtensionValue("--expected-revision from the reviewed plan", applyRevision); err != nil {
			return err
		}
		return a.withConfirmedExtensionServer(cmd.Context(), "Apply region plan "+safeText(args[0])+" to the whole host; DNS can interrupt connectivity, package installation is not fully reversible, and existing processes are not restarted", applyYes, func(c *client.Client, serverID string) error {
			result, err := c.Region(serverID).ApplyRegion(cmd.Context(), &epb.RegionApplyRequest{ServerId: serverID, PlanId: args[0], ExpectedRevision: applyRevision, RequestId: a.id(), Confirm: true})
			if err != nil {
				return fmt.Errorf("region apply returned an error; do not assume settings are unchanged. Use shiki region inspect --server %q to recover pending_operation_id and confirm_before, then region operation <operation-id>: %w", safeText(serverID), err)
			}
			return a.printRegionOperation(result)
		})
	}}
	apply.Flags().StringVar(&applyRevision, "expected-revision", "", "Required: revision from the reviewed plan; stale plans are rejected")
	apply.Flags().BoolVar(&applyYes, "yes", false, "Confirm applying this plan to the whole host")
	root.AddCommand(apply)

	for _, action := range []string{"confirm", "rollback", "operation"} {
		var yes bool
		command := &cobra.Command{Use: action + " <operation-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := requiredExtensionValue("operation ID", args[0]); err != nil {
				return err
			}
			return a.withExtensionServer(cmd.Context(), func(c *client.Client, serverID string) error {
				if action != "operation" {
					label := "Roll back saved settings for operation " + safeText(args[0]) + "; report any settings or package changes that cannot be restored"
					if action == "confirm" {
						label = "Keep the DNS/settings from operation " + safeText(args[0]) + " after reviewing its new-resolution and connectivity checks; stop its timed DNS rollback"
					}
					if err := a.confirm(cmd.Context(), label+" on server "+safeText(serverID), yes); err != nil {
						return err
					}
				}
				request := &epb.RegionOperationRequest{ServerId: serverID, OperationId: args[0], Confirm: action != "operation"}
				var result *epb.RegionOperation
				var err error
				switch action {
				case "confirm":
					request.RequestId = a.id()
					result, err = c.Region(serverID).ConfirmRegion(cmd.Context(), request)
				case "rollback":
					request.RequestId = a.id()
					result, err = c.Region(serverID).RollbackRegion(cmd.Context(), request)
				default:
					result, err = c.Region(serverID).GetRegionOperation(cmd.Context(), request)
				}
				if err != nil {
					return err
				}
				return a.printRegionOperation(result)
			})
		}}
		switch action {
		case "confirm":
			command.Short = "Keep checked changes before the operation's DNS rollback deadline"
		case "rollback":
			command.Short = "Request restoration of the operation's saved settings"
		default:
			command.Short = "Show actual operation state, recovery deadline, and failure details"
		}
		if action != "operation" {
			command.Flags().BoolVar(&yes, "yes", false, "Explicitly confirm this whole-host operation")
		}
		root.AddCommand(command)
	}
	root.AddCommand(a.extensionDiagnoseCommand(false))
	return root
}

func (a *app) withExtensionServer(ctx context.Context, run func(*client.Client, string) error) error {
	return a.withClient(ctx, true, func(c *client.Client) error {
		server, err := c.ResolveServer(ctx, a.server)
		if err != nil {
			return err
		}
		if server.GetConfig().GetId() == "" {
			return errors.New("selected server has no ID")
		}
		return run(c, server.Config.Id)
	})
}

func (a *app) withConfirmedExtensionServer(ctx context.Context, label string, yes bool, run func(*client.Client, string) error) error {
	return a.withExtensionServer(ctx, func(c *client.Client, serverID string) error {
		if err := a.confirm(ctx, label+" on server "+safeText(serverID), yes); err != nil {
			return err
		}
		return run(c, serverID)
	})
}

func requiredExtensionValue(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if value != strings.TrimSpace(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("%s must not contain surrounding whitespace or control characters", label)
	}
	return nil
}

func readRegionConfig(path string) (*epb.RegionConfig, error) {
	if path == "" {
		return nil, errors.New("--file containing explicit RegionConfig JSON is required")
	}
	data, err := readBoundedFile(path, 1<<20)
	if err != nil {
		return nil, err
	}
	config := new(epb.RegionConfig)
	if err := protojson.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("invalid RegionConfig JSON: %w", err)
	}
	if err := validateCLIRegion(config); err != nil {
		return nil, err
	}
	return config, nil
}

func validateCLIRegion(config *epb.RegionConfig) error {
	if config == nil {
		return errors.New("an explicit regional configuration is required")
	}
	if config.Country != "JP" && config.Country != "US" {
		return errors.New("country must be explicitly JP or US; country does not establish location or eligibility")
	}
	if config.Country == "US" && (config.Timezone == "" || config.Locale == "" || len(config.Languages) == 0) {
		return errors.New("US requires an explicit timezone, UTF-8 locale, and languages; none are guessed")
	}
	if config.Timezone != "" {
		if _, err := time.LoadLocation(config.Timezone); err != nil {
			return fmt.Errorf("invalid IANA timezone %q: %w", config.Timezone, err)
		}
	}
	for _, language := range config.Languages {
		if err := requiredExtensionValue("each language", language); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) printRegionOperation(operation *epb.RegionOperation) error {
	if err := a.print(operation); err != nil {
		return err
	}
	switch operation.GetState() {
	case "failed", "rollback_failed", "rollback-failed", "rollback_conflict":
		return fmt.Errorf("region operation %s: %s: %s", safeText(operation.Id), safeText(operation.State), safeText(operation.Reason))
	}
	return nil
}

func (a *app) extensionDiagnoseCommand(browser bool) *cobra.Command {
	var providersFile string
	var allowExternal, allowWebRTC bool
	use := "diagnose"
	args := cobra.NoArgs
	if browser {
		use = "diagnose <profile-id>"
		args = cobra.ExactArgs(1)
	}
	command := &cobra.Command{Use: use, Args: args, Short: "Report observable evidence from the remote host; external probes are off by default", Long: "Report check/state/value/expected/source/vantage/observed_at/limitation, not a risk score or a detection guarantee. Host and remote Chrome are separate observation points. External HTTPS/DNS probes require both --allow-external and explicit provider JSON; there is no implicit provider or fallback. Providers can observe network address, HTTP metadata, and probe DNS names, never account cookies or tokens.", RunE: func(cmd *cobra.Command, args []string) error {
		request, err := readDiagnosticProviders(providersFile, allowExternal)
		if err != nil {
			return err
		}
		request.AllowWebrtc = allowWebRTC
		if browser {
			if err := validateCLIProfileID(args[0]); err != nil {
				return err
			}
			request.ProfileId = args[0]
		}
		return a.withExtensionServer(cmd.Context(), func(c *client.Client, serverID string) error {
			request.ServerId = serverID
			var report *epb.DiagnosticReport
			var err error
			if browser {
				report, err = c.Browser(serverID).DiagnoseBrowser(cmd.Context(), request)
			} else {
				report, err = c.Region(serverID).DiagnoseRegion(cmd.Context(), request)
			}
			if err != nil {
				return err
			}
			return a.print(report)
		})
	}}
	if browser {
		command.Short = "Collect native evidence inside the specified remote Chrome profile, not the local viewer"
	}
	command.Flags().StringVar(&providersFile, "providers", "", "JSON object containing only providers: {\"providers\":[{\"id\":\"mine\",\"kind\":\"ip-echo\",\"url\":\"https://example.com/ip\"}]}")
	command.Flags().BoolVar(&allowExternal, "allow-external", false, "Authorize only the explicitly configured HTTPS/DNS providers and their network metadata exposure")
	if browser {
		command.Flags().BoolVar(&allowWebRTC, "allow-webrtc", false, "Explicitly allow native WebRTC diagnostics; no implicit STUN/TURN provider is used")
	}
	return command
}

func readDiagnosticProviders(path string, allowExternal bool) (*epb.DiagnosticRequest, error) {
	request := new(epb.DiagnosticRequest)
	if path == "" {
		if allowExternal {
			return nil, errors.New("--allow-external requires --providers JSON; no providers are selected automatically")
		}
		return request, nil
	}
	if !allowExternal {
		return nil, errors.New("--providers requires --allow-external; external probes are off by default")
	}
	data, err := readBoundedFile(path, 1<<20)
	if err != nil {
		return nil, err
	}
	if err := protojson.Unmarshal(data, request); err != nil {
		return nil, fmt.Errorf("invalid diagnostic providers JSON: %w", err)
	}
	if request.ServerId != "" || request.ProfileId != "" || request.AllowExternal || request.AllowWebrtc {
		return nil, errors.New("providers JSON must contain only providers; use --server, the profile argument, and --allow-external for scope and consent")
	}
	if len(request.Providers) == 0 {
		return nil, errors.New("--providers must explicitly name at least one provider")
	}
	seen := make(map[string]bool, len(request.Providers))
	for _, provider := range request.Providers {
		if err := requiredExtensionValue("provider ID", provider.GetId()); err != nil {
			return nil, err
		}
		if seen[provider.Id] {
			return nil, fmt.Errorf("duplicate diagnostic provider ID %q", provider.Id)
		}
		seen[provider.Id] = true
		switch provider.Kind {
		case "ip-echo", "geo", "dns-observer":
		default:
			return nil, fmt.Errorf("provider %q kind must be ip-echo, geo, or dns-observer", provider.Id)
		}
		if provider.IpFamily != "" && provider.IpFamily != "ipv4" && provider.IpFamily != "ipv6" {
			return nil, fmt.Errorf("provider %q ip_family must be empty, ipv4, or ipv6", provider.Id)
		}
		if provider.Kind == "dns-observer" && provider.DnsZone == "" {
			return nil, fmt.Errorf("DNS observer provider %q requires an explicit dns_zone", provider.Id)
		}
		u, err := url.Parse(provider.Url)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
			return nil, fmt.Errorf("provider %q requires an explicit HTTPS URL on port 443 without credentials, query, or fragment", provider.Id)
		}
	}
	request.AllowExternal = true
	return request, nil
}
