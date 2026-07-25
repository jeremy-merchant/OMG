//go:build darwin

package platform

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// defaultUserStateDir returns macOS's per-user, per-host state directory.
// Unlike the home directory, this location remains private even when the user
// has intentionally granted another local account access to selected home
// folders. getconf is the POSIX command-line interface to confstr(3).
func defaultUserStateDir() (string, error) {
	output, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_DIR").Output()
	if err != nil {
		return "", fmt.Errorf("resolve Darwin user directory: %w", err)
	}
	directory := filepath.Clean(strings.TrimSpace(string(output)))
	if directory == "." || !filepath.IsAbs(directory) {
		return "", errors.New("Darwin user directory is not absolute")
	}
	return directory, nil
}
