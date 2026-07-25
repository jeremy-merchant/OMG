//go:build windows

package cli

import (
	"path/filepath"
	"testing"
)

func assertPrivateExportFile(t *testing.T, path string) {
	t.Helper()
	sid, err := planCurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePrivatePlanDACL(path, sid); err != nil {
		t.Fatalf("export DACL is not current-user-only: %v", err)
	}
}

func TestWriteNewExportCreatesPrivateDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.html")
	if err := writeNewExport(path, []byte("private")); err != nil {
		t.Fatal(err)
	}
	assertPrivateExportFile(t, path)
}
