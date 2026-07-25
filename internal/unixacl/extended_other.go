//go:build !darwin && !windows

package unixacl

// RejectPayloadACLFD is a no-op outside Darwin, where this package has no
// portable descriptor-level extended ACL representation to validate.
func RejectPayloadACLFD(fd int) error { return nil }

// RejectStateAncestorACLFD is a no-op outside Darwin.
func RejectStateAncestorACLFD(fd int) error { return nil }
