package plugin

import "runtime"

// BuiltinAvailable distinguishes protected built-in IDs from the artifacts this
// platform and host side can actually run. WSL uses the Linux client surface.
func BuiltinAvailable(id, side string) bool {
	if side != "client" && side != "server" {
		return false
	}
	switch id {
	case "sync", "port":
		return true
	case "region":
		return side == "server" && runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
	case "browser":
		return (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") && (runtime.GOOS == "linux" || side == "client" && runtime.GOOS == "darwin")
	default:
		return false
	}
}

func builtinIDs(side string) []string {
	ids := []string{"sync", "port"}
	for _, id := range []string{"region", "browser"} {
		if BuiltinAvailable(id, side) {
			ids = append(ids, id)
		}
	}
	return ids
}
