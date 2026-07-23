//go:build windows

package platform

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// ErrNonLocalWindowsPath is returned before any filesystem-object operation for
// paths that do not name a canonical local Windows drive.
var ErrNonLocalWindowsPath = errors.New("windows path must be canonical and local")

const (
	windowsDriveRemovable = 2
	windowsDriveFixed     = 3
	windowsDriveRemote    = 4
	windowsDriveRAMDisk   = 6
)

var windowsDriveType = func(root string) uint32 {
	name, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0
	}
	return windows.GetDriveType(name)
}

// ValidateLocalWindowsPath rejects UNC, device-namespace, drive-relative, and
// remote-volume paths without inspecting the named filesystem object. It
// accepts only clean, absolute paths rooted at a local drive; drive-letter
// casing is intentionally preserved and does not affect validation.
func ValidateLocalWindowsPath(path string) error {
	if path == "" || strings.Contains(path, "/") || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrNonLocalWindowsPath
	}

	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, `\\`) || strings.HasPrefix(lower, `\??\`) {
		return ErrNonLocalWindowsPath
	}
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' || !isASCIIAlpha(volume[0]) || !strings.HasPrefix(path[len(volume):], `\`) {
		return ErrNonLocalWindowsPath
	}

	if !hasCanonicalDOSComponents(path[len(volume):]) {
		return ErrNonLocalWindowsPath
	}

	switch windowsDriveType(volume + `\`) {
	case windowsDriveRemovable, windowsDriveFixed, windowsDriveRAMDisk:
		return nil
	default:
		return ErrNonLocalWindowsPath
	}
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func hasCanonicalDOSComponents(path string) bool {
	componentStart := 0
	for index := range len(path) + 1 {
		if index != len(path) && path[index] != '\\' {
			continue
		}
		component := path[componentStart:index]
		if component != "" && !isCanonicalDOSComponent(component) {
			return false
		}
		componentStart = index + 1
	}
	return true
}

func isCanonicalDOSComponent(component string) bool {
	if component[len(component)-1] == '.' || component[len(component)-1] == ' ' {
		return false
	}
	if isReservedDOSDeviceName(component) {
		return false
	}
	for index := range len(component) {
		switch component[index] {
		case ':', '"', '*', '<', '>', '?', '|':
			return false
		default:
			if component[index] < 32 {
				return false
			}
		}
	}
	return true

}

func isReservedDOSDeviceName(component string) bool {
	if extension := strings.IndexByte(component, '.'); extension >= 0 {
		component = component[:extension]
	}
	switch {
	case strings.EqualFold(component, "CON"),
		strings.EqualFold(component, "PRN"),
		strings.EqualFold(component, "AUX"),
		strings.EqualFold(component, "NUL"),
		strings.EqualFold(component, "CLOCK$"):
		return true
	case isReservedDOSPortName(component):
		return true
	default:
		return false
	}
}

func isReservedDOSPortName(component string) bool {
	if len(component) == 4 &&
		(strings.EqualFold(component[:3], "COM") || strings.EqualFold(component[:3], "LPT")) &&
		component[3] >= '1' && component[3] <= '9' {
		return true
	}
	return len(component) == 5 &&
		(strings.EqualFold(component[:3], "COM") || strings.EqualFold(component[:3], "LPT")) &&
		component[3] == 0xc2 &&
		(component[4] == 0xb9 || component[4] == 0xb2 || component[4] == 0xb3)
}
