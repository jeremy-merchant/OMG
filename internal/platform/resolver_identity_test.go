package platform

import "testing"

func TestStableIDPreservesDistinctCaseSensitiveRoots(t *testing.T) {
	identity := func(string) bool { return false }
	upper := stableIDForFilesystem(`/projects/Repo`, identity)
	lower := stableIDForFilesystem(`/projects/repo`, identity)
	if upper == lower {
		t.Fatalf("case-sensitive roots merged: %q", upper)
	}
}

func TestStableIDNormalizesCaseInsensitiveFilesystemRoots(t *testing.T) {
	identity := func(string) bool { return true }
	upper := stableIDForFilesystem(`C:\Work\Repo`, identity)
	lower := stableIDForFilesystem(`c:\work\repo`, identity)
	if upper != lower {
		t.Fatalf("case-insensitive roots diverged: %q != %q", upper, lower)
	}
}
