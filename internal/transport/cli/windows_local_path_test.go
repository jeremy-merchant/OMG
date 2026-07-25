//go:build windows

package cli

import "testing"

func TestWindowsSensitivePayloadAndOutputPathsRejectNonLocalNamespaces(t *testing.T) {
	for _, path := range []string{
		`\\server\share\payload.json`,
		`\\?\UNC\server\share\payload.json`,
		`\\?\C:\payload.json`,
		`\\.\PhysicalDrive0`,
		`\??\C:\payload.json`,
	} {
		if _, err := readPrivatePayloadFile(path); err == nil {
			t.Fatalf("non-local payload path %q was accepted", path)
		}
		if _, err := createNewPrivatePlanFile(path); err == nil {
			t.Fatalf("non-local plan output path %q was accepted", path)
		}
		if _, err := createNewPrivateExportFile(path); err == nil {
			t.Fatalf("non-local export output path %q was accepted", path)
		}
		if err := validatePrivatePlanParent(path); err == nil {
			t.Fatalf("non-local plan parent path %q was accepted", path)
		}
	}
}
