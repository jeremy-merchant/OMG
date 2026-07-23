package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func migrationOutputRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func migrationOutputProject(t *testing.T) string {
	t.Helper()
	root := migrationOutputRoot(t)
	t.Setenv("OMG_STORE_PATH", filepath.Join(root, ".omg", "private", "state.db"))
	if exit, output := run(t, "init", "--project", root, "--json"); exit != ExitSuccess {
		t.Fatalf("init exit=%d output=%s", exit, output)
	}
	return root
}

func migrationOutputDirectory(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "output")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	secureMigrationOutputDirectory(t, path)
	return path
}

func TestMigrationPlanOutputCreatesOnlyNewPrivateRegularFile(t *testing.T) {
	root := migrationOutputProject(t)
	outputDir := migrationOutputDirectory(t, root)
	outputPath := filepath.Join(outputDir, "plan.json")

	if exit, output := run(t, "migration", "plan", "--project", root, "--output", outputPath, "--json"); exit != ExitSuccess {
		t.Fatalf("safe destination exit=%d output=%s", exit, output)
	}
	info, err := os.Lstat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("output mode=%v, want private regular file", info.Mode())
	}
	assertPrivateMigrationOutput(t, outputPath)
}

func TestMigrationPlanOutputRejectsUnsafeDestinationsWithoutMutation(t *testing.T) {
	root := migrationOutputProject(t)
	outputDir := migrationOutputDirectory(t, root)
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, output string) func(t *testing.T)
	}{
		{
			name: "existing regular file",
			setup: func(t *testing.T, output string) func(t *testing.T) {
				t.Helper()
				const original = "do not overwrite"
				if err := os.WriteFile(output, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
				return func(t *testing.T) {
					t.Helper()
					data, err := os.ReadFile(output)
					if err != nil || string(data) != original {
						t.Fatalf("existing file changed: %q %v", data, err)
					}
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, output string) func(t *testing.T) {
				t.Helper()
				referent := filepath.Join(filepath.Dir(output), "referent.json")
				const original = "do not follow"
				if err := os.WriteFile(referent, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(referent, output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				return func(t *testing.T) {
					t.Helper()
					info, err := os.Lstat(output)
					if err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Fatalf("symlink changed: %v %v", info, err)
					}
					data, err := os.ReadFile(referent)
					if err != nil || string(data) != original {
						t.Fatalf("referent changed: %q %v", data, err)
					}
				}
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, output string) func(t *testing.T) {
				t.Helper()
				if err := os.Mkdir(output, 0o700); err != nil {
					t.Fatal(err)
				}
				return func(t *testing.T) {
					t.Helper()
					info, err := os.Lstat(output)
					if err != nil || !info.IsDir() {
						t.Fatalf("directory changed: %v %v", info, err)
					}
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(outputDir, strings.ReplaceAll(t.Name(), "/", "_")+".json")
			assertUnchanged := tc.setup(t, output)
			if exit, text := run(t, "migration", "plan", "--project", root, "--output", output, "--json"); exit != ExitUnavailable {
				t.Fatalf("exit=%d output=%s", exit, text)
			}
			assertUnchanged(t)
		})
	}
}
