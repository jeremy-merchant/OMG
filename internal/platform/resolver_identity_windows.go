//go:build windows

package platform

import (
	"os"
	"strings"
)

// caseInsensitiveProjectRoot reports whether an existing case variant names the
// same directory. This avoids treating two directories on a case-sensitive
// Windows volume as one project while retaining stable IDs on normal NTFS.
func caseInsensitiveProjectRoot(root string) bool {
	info, err := os.Stat(root)
	if err != nil {
		return false
	}

	variant, ok := caseVariant(root)
	if !ok {
		return false
	}
	variantInfo, err := os.Stat(variant)
	return err == nil && os.SameFile(info, variantInfo)
}

func caseVariant(path string) (string, bool) {
	for _, candidate := range []string{strings.ToLower(path), strings.ToUpper(path)} {
		if candidate != path {
			return candidate, true
		}
	}
	return "", false
}
