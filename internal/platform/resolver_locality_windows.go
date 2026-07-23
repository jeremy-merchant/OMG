//go:build windows

package platform

// validateResolvedProjectRoot rejects non-local project roots before configuration
// loading can inspect project-controlled filesystem paths.
func validateResolvedProjectRoot(root string) error {
	return ValidateLocalWindowsPath(root)
}
