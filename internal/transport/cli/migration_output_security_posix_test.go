//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func assertPrivateMigrationOutput(t *testing.T, path string) {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("output permissions=%#o err=%v, want 0600", info.Mode().Perm(), err)
	}
}

func secureMigrationOutputDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationPlanOutputRejectsBroadParent(t *testing.T) {
	root := migrationOutputProject(t)
	parent := filepath.Join(root, "broad")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(parent, "plan.json")
	if exit, text := run(t, "migration", "plan", "--project", root, "--output", output, "--json"); exit != ExitUnavailable {
		t.Fatalf("exit=%d output=%s", exit, text)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("output created in broad parent: %v", err)
	}
}

func TestMigrationPlanOutputRejectsFIFOWithoutMutation(t *testing.T) {
	root := migrationOutputProject(t)
	parent := migrationOutputDirectory(t, root)
	output := filepath.Join(parent, "plan.fifo")
	if err := syscall.Mkfifo(output, 0o600); err != nil {
		t.Fatal(err)
	}

	if exit, text := run(t, "migration", "plan", "--project", root, "--output", output, "--json"); exit != ExitUnavailable {
		t.Fatalf("exit=%d output=%s", exit, text)
	}
	info, err := os.Lstat(output)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO changed: mode=%v err=%v", info.Mode(), err)
	}
}
