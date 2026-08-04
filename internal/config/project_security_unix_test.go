//go:build !windows

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremy-merchant/oh-my-group/internal/config"
	"golang.org/x/sys/unix"
)

func TestLoadRejectsUnsafeConfigurationFiles(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, path string) {
				target := filepath.Join(t.TempDir(), "target.toml")
				if err := os.WriteFile(target, []byte("[project]\nname=\"outside\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "fifo",
			setup: func(t *testing.T, path string) {
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "oversized",
			setup: func(t *testing.T, path string) {
				if err := os.WriteFile(path, make([]byte, (1<<20)+1), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, ".omg")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			test.setup(t, filepath.Join(directory, "project.toml"))
			if _, err := config.Load(root); err == nil {
				t.Fatal("unsafe project configuration was accepted")
			}
		})
	}
}

func TestLoadRejectsBroadLocalOverride(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, ".omg")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "project.toml"), []byte("[project]\nname=\"tracked\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(directory, "local.toml")
	if err := os.WriteFile(local, []byte("[project]\ndisplay=\"local\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(root); err == nil {
		t.Fatal("broad local override was accepted")
	}
}
