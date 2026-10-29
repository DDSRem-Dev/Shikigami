// Package buildinfo holds release metadata shared by host and plugin binaries.
// Application protocol compatibility is negotiated separately from these labels.
package buildinfo

import (
	"fmt"
	"runtime"
)

// Release builds set these through -ldflags -X. Development builds stay explicit.
var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

type Information struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuiltAt      string `json:"built_at"`
	GoVersion    string `json:"go_version"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

func Current() Information {
	return Information{Version: Version, Commit: Commit, BuiltAt: BuiltAt, GoVersion: runtime.Version(), OS: runtime.GOOS, Architecture: runtime.GOARCH}
}

func String(binary string) string {
	return fmt.Sprintf("%s %s (%s, %s/%s, %s)", binary, Version, Commit, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
