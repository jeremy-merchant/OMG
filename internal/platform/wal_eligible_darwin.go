//go:build darwin

package platform

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// WALEligible returns true only for filesystems confidently known to provide
// local SQLite locking semantics. Unknown and network filesystems use DELETE.
func WALEligible(path string) bool {
	root, ok := existingDirectory(path)
	if !ok {
		return false
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(root, &stat); err != nil {
		return false
	}
	kind := strings.ToLower(string(bytes.TrimRight(stat.Fstypename[:], "\x00")))
	switch kind {
	case "apfs", "hfs", "ufs":
		return true
	default:
		return false
	}
}

func existingDirectory(path string) (string, bool) {
	for candidate := filepath.Clean(path); ; candidate = filepath.Dir(candidate) {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", false
		}
	}
}
