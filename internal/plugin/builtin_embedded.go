//go:build shiki_embedded

package plugin

import (
	"embed"
	"io/fs"
)

// Generated locally by scripts/build-embedded.sh for exactly one target.
//
//go:embed embedded-assets/*
var embeddedBuiltinFS embed.FS

func builtinAssets() fs.FS { assets, _ := fs.Sub(embeddedBuiltinFS, "embedded-assets"); return assets }
