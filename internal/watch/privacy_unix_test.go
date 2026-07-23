//go:build !windows

package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func newPrivateStateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newBroadStateDir(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "unsafe")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writePrivateTestFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
