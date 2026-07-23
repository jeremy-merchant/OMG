//go:build darwin

package platform

import (
	"os"
	"strings"
)

// caseInsensitiveProjectRoot reports whether a differently cased spelling of
// an existing project root resolves to the same directory on this volume.
func caseInsensitiveProjectRoot(root string) bool {
	info, err := os.Stat(root)
	if err != nil {
		return false
	}

	for _, variant := range []string{strings.ToLower(root), strings.ToUpper(root)} {
		if variant == root {
			continue
		}
		variantInfo, err := os.Stat(variant)
		if err == nil && os.SameFile(info, variantInfo) {
			return true
		}
	}
	return false
}
