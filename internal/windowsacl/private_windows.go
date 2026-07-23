//go:build windows

// Package windowsacl validates the owner-only Windows security descriptor used
// by OMG's private state, watch, and sensitive payload files.
package windowsacl

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsPrivate reports whether descriptor grants exactly full access to sid and
// nothing else. DACL string formatting and control-flag serialization vary
// across native Windows and compatibility layers, so access is compared by ACE.
func IsPrivate(descriptor *windows.SECURITY_DESCRIPTOR, sid *windows.SID) bool {
	if descriptor == nil || sid == nil {
		return false
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(sid) {
		return false
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil || dacl == nil || defaulted || dacl.AceCount != 1 {
		return false
	}
	expectedDescriptor, err := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:(A;;FA;;;" + sid.String() + ")")
	if err != nil {
		return false
	}
	expectedDACL, _, err := expectedDescriptor.DACL()
	if err != nil || expectedDACL == nil || expectedDACL.AceCount != 1 {
		return false
	}
	var actual, expected *windows.ACCESS_ALLOWED_ACE
	if windows.GetAce(dacl, 0, &actual) != nil || windows.GetAce(expectedDACL, 0, &expected) != nil || actual == nil || expected == nil {
		return false
	}
	actualSID := (*windows.SID)(unsafe.Pointer(&actual.SidStart))
	return actual.Header.AceType == expected.Header.AceType &&
		actual.Header.AceFlags == expected.Header.AceFlags &&
		actual.Mask == expected.Mask && sid.Equals(actualSID)
}
