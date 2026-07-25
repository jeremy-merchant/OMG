//go:build !windows

package platform

func validateResolvedProjectRoot(string) error { return nil }
