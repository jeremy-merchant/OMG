//go:build !darwin && !windows

package platform

// WALEligible fails closed where the platform adapter has no confident local
// filesystem classification.
func WALEligible(string) bool { return false }
