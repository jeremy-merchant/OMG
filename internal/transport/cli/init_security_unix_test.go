//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitRejectsDanglingProjectConfigSymlink(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, ".omg")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.toml")
	if err := os.Symlink(target, filepath.Join(directory, "project.toml")); err != nil {
		t.Fatal(err)
	}
	exit, _ := run(t, "init", "--project", root, "--json")
	if exit == ExitSuccess {
		t.Fatal("init accepted dangling project configuration symlink")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("init wrote through dangling symlink: %v", err)
	}
}
