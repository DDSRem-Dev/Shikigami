//go:build !linux

package install

import (
	"context"
	"errors"
)

func extensionPlatformPlan(context.Context, string, ExtensionOptions) (*ExtensionPlan, error) {
	return nil, errors.New("region/browser system setup and maintenance require Ubuntu 24.04 or Debian 12 on Linux amd64/arm64; client plugins remain available on supported client platforms")
}

func applyExtensionPlan(context.Context, string, ExtensionOptions) error {
	return errors.New("system plugin support provisioning is Linux-only")
}

func (n *Native) removeInstalledExtensions(context.Context, Plan) error { return nil }
