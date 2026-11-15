//go:build !linux

package install

import (
	"context"
	"errors"
)

func InspectQuickstartSupport(context.Context, ExtensionOptions) (QuickstartSupport, error) {
	return QuickstartSupport{}, errors.New("system plugin support requires Linux")
}
