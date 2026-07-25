//go:build !windows && !darwin

package platform

func caseInsensitiveProjectRoot(string) bool { return false }
