//go:build !windows

package instructions

import (
	"os"
	"testing"
)

func assertPreservedInstructionMode(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o751 {
		t.Fatalf("mode = %v, err=%v", info.Mode(), err)
	}
}
