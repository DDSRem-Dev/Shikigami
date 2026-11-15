package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"github.com/spf13/cobra"
)

func (a *app) browserCommands() *cobra.Command {
	root := &cobra.Command{Use: "browser", Short: "Manage persistent remote Chrome profiles and local, loopback-only viewers", Long: "Manage remote Chrome through the local agent, never by connecting directly to a daemon. Chrome continues after a CLI, viewer, or agent disconnect. Stopping Chrome is a separate explicit action. A viewer carries the remote display, not a local replacement for remote Chrome. Use the viewer's explicit Take control action to acquire its versioned attachment; clipboard and file actions are separately authorized.", Args: cobra.NoArgs}
	root.AddCommand(a.browserProfileCommands(), a.browserOpenCommand(), a.browserFileCommands(), a.extensionDiagnoseCommand(true))
	status := &cobra.Command{Use: "status [profile-id]", Args: cobra.MaximumNArgs(1), Short: "Show the actual managed instance, controller revision, restart needs, and failures", RunE: func(cmd *cobra.Command, args []string) error {
		profileID := ""
		if len(args) != 0 {
			profileID = args[0]
			if err := validateCLIProfileID(profileID); err != nil {
				return err
			}
		}
		return a.withExtensionServer(cmd.Context(), func(c *client.Client, serverID string) error {
			result, err := c.Browser(serverID).GetBrowserStatus(cmd.Context(), &epb.BrowserScope{ServerId: serverID, ProfileId: profileID})
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	root.AddCommand(status)
	for _, action := range []string{"start", "stop"} {
		var yes, replaceRunning bool
		command := &cobra.Command{Use: action + " <profile-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCLIProfileID(args[0]); err != nil {
				return err
			}
			label := "Stop remote Chrome profile " + safeText(args[0]) + "; profile data remains, but unsaved pages may be lost"
			if action == "start" {
				label = "Start remote Chrome profile " + safeText(args[0]) + " with its saved regional settings"
				if replaceRunning {
					label += "; stop any other running profile first (unsaved pages may be lost)"
				}
			}
			return a.withConfirmedExtensionServer(cmd.Context(), label, yes, func(c *client.Client, serverID string) error {
				request := &epb.BrowserActionRequest{ServerId: serverID, ProfileId: args[0], RequestId: a.id(), Confirm: true, ReplaceRunning: replaceRunning}
				var result *epb.BrowserStatus
				var err error
				if action == "start" {
					result, err = c.Browser(serverID).StartBrowser(cmd.Context(), request)
				} else {
					result, err = c.Browser(serverID).StopBrowser(cmd.Context(), request)
				}
				if err != nil {
					return err
				}
				if err := a.print(result); err != nil {
					return err
				}
				if action == "start" && (result.GetState() == "failed" || result.GetState() == "crashed") {
					return fmt.Errorf("browser profile %s is %s: %s; inspect browser status and restart explicitly", safeText(args[0]), safeText(result.State), safeText(result.Reason))
				}
				return nil
			})
		}}
		if action == "start" {
			command.Short = "Start one profile; never silently replace an existing managed instance"
			command.Flags().BoolVar(&replaceRunning, "replace-running", false, "Explicitly permit stopping another profile before starting this one")
		} else {
			command.Short = "Stop the specified managed instance while retaining its private profile data"
		}
		command.Flags().BoolVar(&yes, "yes", false, "Confirm this remote Chrome lifecycle change")
		root.AddCommand(command)
	}
	closeViewer := &cobra.Command{Use: "close-viewer <viewer-id>", Args: cobra.ExactArgs(1), Short: "Close a local viewer and its attachment; do not stop remote Chrome", RunE: func(cmd *cobra.Command, args []string) error {
		if a.server != "" {
			return errors.New("close-viewer is local and uses only its viewer ID; omit --server")
		}
		if err := requiredExtensionValue("viewer ID", args[0]); err != nil {
			return err
		}
		return a.withClient(cmd.Context(), true, func(c *client.Client) error {
			result, err := c.BrowserLocal().CloseBrowserViewer(cmd.Context(), &epb.CloseBrowserViewerRequest{ViewerId: args[0]})
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	root.AddCommand(closeViewer)
	return root
}

func (a *app) browserProfileCommands() *cobra.Command {
	root := &cobra.Command{Use: "profile", Short: "Manage private persistent Chrome profiles and their single explicit boot target", Args: cobra.NoArgs}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List profile IDs, names, revisions, saved settings, and boot selection", RunE: func(cmd *cobra.Command, _ []string) error {
		return a.withExtensionServer(cmd.Context(), func(c *client.Client, serverID string) error {
			result, err := c.Browser(serverID).ListBrowserProfiles(cmd.Context(), &epb.BrowserScope{ServerId: serverID})
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	root.AddCommand(list)
	for _, action := range []string{"create", "update"} {
		var name, regionFile, revision string
		var yes, inheritRegion bool
		command := &cobra.Command{Use: action + " <profile-id>", Args: cobra.ExactArgs(1), Short: "Save profile settings without starting Chrome or enabling boot startup", RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCLIProfileID(args[0]); err != nil {
				return err
			}
			if action == "create" && revision != "" {
				return errors.New("profile create cannot have an existing --expected-revision; use profile update")
			}
			if name != "" {
				if err := requiredExtensionValue("profile name", name); err != nil {
					return err
				}
			}
			if action == "create" && name == "" {
				return errors.New("profile create requires --name; optional --file supplies explicit RegionConfig, otherwise settings inherit from the selected server at launch")
			}
			if regionFile != "" && inheritRegion {
				return errors.New("--file and --inherit-region are mutually exclusive")
			}
			if action == "update" && !cmd.Flags().Changed("name") && regionFile == "" && !inheritRegion {
				return errors.New("profile update requires --name, --file containing RegionConfig JSON, or --inherit-region")
			}
			if cmd.Flags().Changed("name") && name == "" {
				return errors.New("profile name must not be empty")
			}
			var region *epb.RegionConfig
			if regionFile != "" {
				var err error
				region, err = readRegionConfig(regionFile)
				if err != nil {
					return err
				}
			}
			return a.withConfirmedExtensionServer(cmd.Context(), action+" remote Chrome profile "+safeText(args[0])+"; existing Chrome processes are not restarted and profile data is not exported", yes, func(c *client.Client, serverID string) error {
				profile := &epb.BrowserProfile{Id: args[0], Name: name, Region: region}
				expected := revision
				if action == "update" {
					current, err := findCLIProfile(cmd.Context(), c, serverID, args[0])
					if err != nil {
						return err
					}
					profile = editableCLIProfile(current)
					if name != "" {
						profile.Name = name
					}
					if region != nil || inheritRegion {
						profile.Region = region
					}
					if expected == "" {
						expected = current.Revision
					}
					if expected == "" {
						return errors.New("profile has no observed revision; a safe update cannot be submitted")
					}
				}
				result, err := c.Browser(serverID).PutBrowserProfile(cmd.Context(), &epb.PutBrowserProfileRequest{ServerId: serverID, Profile: profile, ExpectedRevision: expected, RequestId: a.id()})
				if err != nil {
					return err
				}
				return a.print(result)
			})
		}}
		command.Flags().StringVar(&name, "name", "", "Profile display name (required on create)")
		command.Flags().StringVar(&regionFile, "file", "", "Explicit RegionConfig JSON; create without it inherits from the server at launch, update without it preserves settings")
		command.Flags().BoolVar(&inheritRegion, "inherit-region", false, "Explicitly clear saved regional overrides and inherit the server configuration at launch")
		command.Flags().StringVar(&revision, "expected-revision", "", "Update only this observed revision; defaults to a fresh profile read")
		command.Flags().BoolVar(&yes, "yes", false, "Confirm persisting the profile configuration")
		root.AddCommand(command)
	}
	var deleteRevision string
	var deleteYes bool
	remove := &cobra.Command{Use: "delete <profile-id>", Args: cobra.ExactArgs(1), Short: "Stop the managed instance and delete its private cookies, settings, and staging data", RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateCLIProfileID(args[0]); err != nil {
			return err
		}
		return a.withConfirmedExtensionServer(cmd.Context(), "Permanently delete remote Chrome profile "+safeText(args[0])+", including cookies, settings, and staging files; stop its managed instance if running (unsaved pages may be lost)", deleteYes, func(c *client.Client, serverID string) error {
			expected := deleteRevision
			if expected == "" {
				profile, err := findCLIProfile(cmd.Context(), c, serverID, args[0])
				if err != nil {
					return err
				}
				expected = profile.Revision
			}
			if expected == "" {
				return errors.New("profile delete requires an observed --expected-revision")
			}
			result, err := c.Browser(serverID).DeleteBrowserProfile(cmd.Context(), &epb.DeleteBrowserProfileRequest{ServerId: serverID, ProfileId: args[0], ExpectedRevision: expected, RequestId: a.id(), Confirm: true})
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	remove.Flags().StringVar(&deleteRevision, "expected-revision", "", "Require this observed revision; defaults to a fresh profile read")
	remove.Flags().BoolVar(&deleteYes, "yes", false, "Confirm permanently deleting this stopped profile")
	root.AddCommand(remove)

	var bootRevision string
	var bootEnabled, replaceBoot, bootYes bool
	boot := &cobra.Command{Use: "boot <profile-id>", Args: cobra.ExactArgs(1), Short: "Explicitly select or clear a profile's boot startup; do not start it now", RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateCLIProfileID(args[0]); err != nil {
			return err
		}
		if !bootEnabled && replaceBoot {
			return errors.New("--replace-boot is valid only with --enabled=true")
		}
		label := "Clear boot startup for remote Chrome profile " + safeText(args[0])
		if bootEnabled {
			label = "Select remote Chrome profile " + safeText(args[0]) + " as the single boot target; startup requires an enabled, validated browser plugin"
			if replaceBoot {
				label += "; replace any other selected boot profile"
			}
		}
		return a.withConfirmedExtensionServer(cmd.Context(), label, bootYes, func(c *client.Client, serverID string) error {
			current, err := findCLIProfile(cmd.Context(), c, serverID, args[0])
			if err != nil {
				return err
			}
			expected := bootRevision
			if expected == "" {
				expected = current.Revision
			}
			if expected == "" {
				return errors.New("boot selection requires an observed profile revision")
			}
			profile := editableCLIProfile(current)
			profile.BootStart = bootEnabled
			result, err := c.Browser(serverID).PutBrowserProfile(cmd.Context(), &epb.PutBrowserProfileRequest{ServerId: serverID, Profile: profile, ExpectedRevision: expected, RequestId: a.id(), ReplaceBootProfile: replaceBoot})
			if err != nil {
				return err
			}
			return a.print(result)
		})
	}}
	boot.Flags().BoolVar(&bootEnabled, "enabled", true, "Select this boot target; --enabled=false clears it")
	boot.Flags().BoolVar(&replaceBoot, "replace-boot", false, "Explicitly permit clearing another profile's boot selection")
	boot.Flags().StringVar(&bootRevision, "expected-revision", "", "Require this observed revision; defaults to a fresh profile read")
	boot.Flags().BoolVar(&bootYes, "yes", false, "Confirm changing boot startup selection")
	root.AddCommand(boot)
	return root
}

func validateCLIProfileID(id string) error {
	if err := requiredExtensionValue("profile ID", id); err != nil {
		return err
	}
	if len(id) > 128 || strings.ContainsAny(id, "/\\") || id == "." || id == ".." {
		return errors.New("profile ID must be a single identifier, not a path, and at most 128 bytes")
	}
	return nil
}

func findCLIProfile(ctx context.Context, c *client.Client, serverID, profileID string) (*epb.BrowserProfile, error) {
	list, err := c.Browser(serverID).ListBrowserProfiles(ctx, &epb.BrowserScope{ServerId: serverID})
	if err != nil {
		return nil, err
	}
	for _, profile := range list.GetProfiles() {
		if profile.GetId() == profileID {
			return profile, nil
		}
	}
	return nil, fmt.Errorf("browser profile %q was not found on the selected server", profileID)
}

func editableCLIProfile(profile *epb.BrowserProfile) *epb.BrowserProfile {
	return &epb.BrowserProfile{Id: profile.Id, Name: profile.Name, BootStart: profile.BootStart, Region: profile.Region}
}

func (a *app) browserOpenCommand() *cobra.Command {
	var readOnly, launch bool
	command := &cobra.Command{Use: "open <profile-id>", Args: cobra.ExactArgs(1), Short: "Return a short-lived local viewer bootstrap URL; launch a browser only with --launch", Long: "Open a loopback viewer managed by the local browser plugin. Its URL is a short-lived, single-use credential: keep stdout private and do not put it in logs. This command never starts remote Chrome. Closing the CLI does not close the viewer or stop Chrome. Viewers default to read-only; use --read-only=false and the viewer's explicit Take control action for input. Only --launch opens an OS browser.", RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateCLIProfileID(args[0]); err != nil {
			return err
		}
		return a.withExtensionServer(cmd.Context(), func(c *client.Client, serverID string) error {
			viewer, err := c.BrowserLocal().OpenBrowserViewer(cmd.Context(), &epb.OpenBrowserViewerRequest{ServerId: serverID, ProfileId: args[0], ReadOnly: readOnly})
			if err != nil {
				return err
			}
			if err := validateCLIViewerURL(viewer.GetUrl()); err != nil {
				return err
			}
			if err := a.print(viewer); err != nil {
				return err
			}
			if launch {
				return launchCLIViewer(cmd.Context(), viewer.Url)
			}
			return nil
		})
	}}
	command.Flags().BoolVar(&readOnly, "read-only", true, "Read-only viewer; set false to allow an explicit Take control action in the viewer")
	command.Flags().BoolVar(&launch, "launch", false, "Explicitly launch the OS browser with the returned private bootstrap URL")
	return command
}

func validateCLIViewerURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Hostname() == "" {
		return errors.New("viewer returned an invalid local bootstrap URL (URL withheld)")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("viewer returned a non-loopback bootstrap URL (URL withheld)")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return errors.New("viewer returned an invalid local listener port (URL withheld)")
	}
	return nil
}

func launchCLIViewer(ctx context.Context, viewerURL string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", viewerURL)
	case "linux":
		command = exec.CommandContext(ctx, "xdg-open", viewerURL)
	default:
		return errors.New("automatic viewer launch is supported on macOS/Linux/WSL2 only; open the private URL from stdout manually")
	}
	// Do not connect subprocess output to the CLI: launcher errors can echo the
	// single-use credential. The URL is returned only in the requested result.
	if err := command.Run(); err != nil {
		return errors.New("could not launch the OS browser; open the private URL from stdout manually (launcher output withheld)")
	}
	return nil
}
