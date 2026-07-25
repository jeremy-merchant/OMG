//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func assertPrivateExportFile(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("export permissions: mode=%v err=%v", info.Mode(), err)
	}
}

func TestWriteNewExportRejectsSymlinkAncestor(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeNewExport(filepath.Join(link, "board.html"), []byte("private")); err == nil {
		t.Fatal("export accepted a symlink ancestor")
	}
	if _, err := os.Stat(filepath.Join(target, "board.html")); !os.IsNotExist(err) {
		t.Fatalf("export escaped through symlink ancestor: %v", err)
	}
}
