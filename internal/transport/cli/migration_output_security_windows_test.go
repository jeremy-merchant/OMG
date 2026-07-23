//go:build windows

package cli

import (
	"os"
	"testing"
)

func assertPrivateMigrationOutput(t *testing.T, path string) {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("output mode=%v err=%v, want regular file", info.Mode(), err)
	}
	sid, err := planCurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePrivatePlanDACL(path, sid); err != nil {
		t.Fatalf("output DACL is not private: %v", err)
	}
}

func secureMigrationOutputDirectory(t *testing.T, path string) {
	t.Helper()
	sid, err := planCurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	setPayloadDACL(t, path, "O:"+sid.String()+"D:P(A;;FA;;;"+sid.String()+")")
}
