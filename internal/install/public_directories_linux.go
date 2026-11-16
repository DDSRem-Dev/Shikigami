//go:build linux

package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func publicSupportDirectories(o ExtensionOptions) []string {
	directories := []string{filepath.Dir(BrowserRuntimePath)}
	if o.includes("browser") {
		directories = append(directories, chromePolicyDirectories()...)
	}
	if o.BootUnlock {
		directories = append(directories, "/etc/shikigami")
	}
	return directories
}

// A restrictive application directory can be repaired after approval. Shared
// ancestors are never broadened, even if an older installer may have created
// them: there is no ownership receipt for unrelated applications' directories.
func publicSupportDirectoryState(directories []string) (bool, error) {
	repairable := make(map[string]bool, len(directories))
	for _, directory := range directories {
		repairable[directory] = true
	}
	current := true
	for _, directory := range directories {
		for path := directory; ; path = filepath.Dir(path) {
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				current = false
			} else if err != nil {
				return false, err
			} else {
				owner, ok := info.Sys().(*syscall.Stat_t)
				if !ok || !info.IsDir() || owner.Uid != 0 || info.Mode().Perm()&0022 != 0 {
					return false, fmt.Errorf("unsafe public support directory or ancestor %s; preserve it for administrator review", path)
				}
				if repairable[path] {
					current = current && info.Mode().Perm() == 0755
				} else if info.Mode().Perm()&0001 == 0 {
					return false, fmt.Errorf("shared ancestor %s is not searchable by ordinary users; preserve its permissions for administrator review", path)
				}
			}
			if path == "/" {
				break
			}
		}
	}
	return current, nil
}

func appendPublicSupportDirectoryPlan(plan *ExtensionPlan, o ExtensionOptions) {
	if o.StageOnly {
		return
	}
	directories := publicSupportDirectories(o)
	plan.Paths = append(plan.Paths, directories...)
	plan.Warnings = append(plan.Warnings, "Approved setup sets these public application directories to mode 0755: "+strings.Join(directories, ", ")+". Existing shared ancestors such as /etc/opt retain their permissions; a restrictive shared ancestor blocks setup for administrator review. Credential directories remain 0700 and credential files 0600; no recursive permission change.")
	if _, err := publicSupportDirectoryState(directories); err != nil {
		plan.Blocked = append(plan.Blocked, err.Error())
	}
}
