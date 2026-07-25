//go:build windows

package platform

// WALEligible fails closed on Windows because this process does not classify
// volume types with sufficient confidence to distinguish local and network IO.
func WALEligible(string) bool { return false }
