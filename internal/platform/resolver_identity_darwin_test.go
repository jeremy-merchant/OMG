//go:build darwin

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStableIDDarwinNormalizesCaseAliasesOnInsensitiveVolume(t *testing.T) {
	root := filepath.Join(t.TempDir(), "CaseProbe")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}

	alias := filepath.Join(filepath.Dir(root), strings.ToLower(filepath.Base(root)))
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(alias)
	if err != nil || !os.SameFile(rootInfo, aliasInfo) {
		t.Skip("filesystem does not resolve case aliases to the same directory")
	}

	if !caseInsensitiveProjectRoot(root) {
		t.Fatal("case-insensitive filesystem was not detected")
	}
	if rootID, aliasID := stableID(root), stableID(alias); rootID != aliasID {
		t.Fatalf("case aliases produced different IDs: %q != %q", rootID, aliasID)
	}
}
