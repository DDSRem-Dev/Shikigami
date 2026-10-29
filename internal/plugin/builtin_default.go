//go:build !shiki_embedded

package plugin

import "io/fs"

func builtinAssets() fs.FS { return nil }
