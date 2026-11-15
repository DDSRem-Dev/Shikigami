//go:build linux

package runtime

import (
	"context"
	"errors"
	"strings"
	"time"
)

func chromeLanguages(values []string) []string {
	languages := make([]string, len(values))
	for index, value := range values {
		languages[index] = strings.ReplaceAll(value, "_", "-")
	}
	return languages
}
func checkInstalledLocales(parent context.Context, environment []string) error {
	if err := verifyExecutable("/usr/bin/locale"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	data, err := command(ctx, "/usr/bin/locale", "--all-locales")
	if err != nil {
		return err
	}
	canonical := func(value string) string { return strings.ToLower(strings.ReplaceAll(value, "-", "")) }
	installed := make(map[string]bool)
	for _, value := range strings.Fields(string(data)) {
		installed[canonical(value)] = true
	}
	all := ""
	for _, entry := range environment {
		if strings.HasPrefix(entry, "LC_ALL=") {
			all = strings.TrimPrefix(entry, "LC_ALL=")
		}
	}
	if all != "" {
		if !installed[canonical(all)] {
			return errors.New("effective LC_ALL locale " + all + " is not installed/generated")
		}
		return nil
	}
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		if value != "" && (key == "LANG" || strings.HasPrefix(key, "LC_")) && !installed[canonical(value)] {
			return errors.New("effective locale " + value + " is not generated; explicitly install/generate it through the administrator region dependency plan")
		}
	}
	return nil
}
