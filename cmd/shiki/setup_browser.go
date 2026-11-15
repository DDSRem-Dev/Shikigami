package main

import (
	"context"
	"errors"
	"fmt"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
)

func (w *setupWizard) browser(ctx context.Context, server string) error {
	profiles := new(epb.BrowserProfileList)
	if err := w.backend.query(ctx, profiles, "--server", server, "browser", "profile", "list"); err != nil {
		return err
	}
	if len(profiles.Profiles) != 0 {
		fmt.Fprintln(w.out, "Existing browser profiles preserved. Select one explicitly to open or resume it; setup never replaces another running profile.")
		for _, profile := range profiles.Profiles {
			fmt.Fprintf(w.out, "  Profile: %s\n", safeText(profile.Id))
		}
		id, err := w.prompt(ctx, "Open existing profile ID (blank leaves browsers unchanged)", "")
		if err != nil || id == "" {
			return err
		}
		found := false
		for _, profile := range profiles.Profiles {
			found = found || profile.Id == id
		}
		if !found {
			return errors.New("choose an existing profile ID")
		}
		status := new(epb.BrowserStatus)
		if err := w.backend.query(ctx, status, "--server", server, "browser", "status", id); err != nil {
			return err
		}
		if status.State != "running" {
			answer, err := w.prompt(ctx, "Start this saved browser profile? yes/no", "yes")
			if err != nil {
				return err
			}
			if answer != "yes" {
				fmt.Fprintln(w.out, "Saved profile retained; browser start remains pending.")
				return nil
			}
			return w.startBrowser(ctx, server, id)
		}
		return w.openBrowser(ctx, server, id)
	}
	answer, err := w.prompt(ctx, "Create a persistent browser profile named work, start Chrome and open its local viewer? yes/no", "yes")
	if err != nil {
		return err
	}
	if answer != "yes" {
		fmt.Fprintln(w.out, "Browser plugins enabled; no profile started or viewer verified.")
		return nil
	}
	region, err := w.browserRegion(ctx)
	if err != nil {
		return err
	}
	create := []string{"--server", server, "browser", "profile", "create", "work", "--name", "Work", "--yes"}
	if region == nil {
		err = w.backend.run(ctx, false, create...)
	} else {
		err = setupJSONFile(region, func(path string) error {
			return w.backend.run(ctx, false, append(create, "--file", path)...)
		})
	}
	if err != nil {
		return err
	}
	return w.startBrowser(ctx, server, "work")
}

func (w *setupWizard) startBrowser(ctx context.Context, server, id string) error {
	if err := w.backend.run(ctx, false, "--server", server, "browser", "start", id, "--yes"); err != nil {
		return fmt.Errorf("browser profile saved, but start remains pending; inspect browser status and rerun the VPS quickstart for missing system/keyring support: %w", err)
	}
	return w.openBrowser(ctx, server, id)
}

func (w *setupWizard) openBrowser(ctx context.Context, server, id string) error {
	if err := w.backend.run(ctx, true, "--server", server, "browser", "open", id, "--read-only=false", "--launch"); err != nil {
		return fmt.Errorf("Chrome started, but local viewer opening failed: %w", err)
	}
	fmt.Fprintln(w.out, "Use Take control in the viewer to type. Website/Claude login is completed by you in that remote browser. Closing the viewer leaves Chrome running; automatic boot startup is not enabled.")
	return nil
}

func (w *setupWizard) browserRegion(ctx context.Context) (map[string]any, error) {
	choice, err := w.prompt(ctx, "Browser region/language: inherit (VPS settings), jp (Japanese), or us (US English)", "inherit")
	if err != nil {
		return nil, err
	}
	switch choice {
	case "inherit":
		return nil, nil
	case "jp":
		return map[string]any{"country": "JP", "timezone": "Asia/Tokyo", "locale": "ja_JP.UTF-8", "languages": []string{"ja_JP", "ja"}}, nil
	case "us":
		timezone, err := w.prompt(ctx, "US browser IANA timezone (e.g. America/New_York)", "")
		if err != nil {
			return nil, err
		}
		if timezone == "" {
			return nil, errors.New("US timezone must be chosen explicitly")
		}
		return map[string]any{"country": "US", "timezone": timezone, "locale": "en_US.UTF-8", "languages": []string{"en_US", "en"}}, nil
	default:
		return nil, errors.New("browser region must be inherit, jp or us")
	}
}
