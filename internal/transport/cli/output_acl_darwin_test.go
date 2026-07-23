//go:build darwin

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func privateOutputACLRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPrivateOutputCreatorsRejectInheritedDangerousACLWithoutRetainingFile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		create func(string) (*os.File, error)
	}{
		{name: "export", create: createNewPrivateExportFile},
		{name: "migration plan", create: createNewPrivatePlanFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := privateOutputACLRoot(t)
			setDarwinInheritedReadACL(t, parent)
			path := filepath.Join(parent, "private-output.json")

			file, err := tc.create(path)
			if file != nil {
				_ = file.Close()
			}
			if err == nil {
				t.Fatal("creator accepted output inheriting an everyone-read ACL")
			}
			if info, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
				t.Fatalf("rejected output retained: info=%v err=%v", info, statErr)
			}
		})
	}
}

func TestPrivateOutputCreatorsKeepSafeOutputPrivate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		create func(string) (*os.File, error)
	}{
		{name: "export", create: createNewPrivateExportFile},
		{name: "migration plan", create: createNewPrivatePlanFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(privateOutputACLRoot(t), "private-output.json")
			file, err := tc.create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("safe output permissions=%#o err=%v, want 0600", info.Mode().Perm(), err)
			}
		})
	}
}

func TestPrivateOutputCreatorsPreserveExistingDestination(t *testing.T) {
	for _, tc := range []struct {
		name   string
		create func(string) (*os.File, error)
	}{
		{name: "export", create: createNewPrivateExportFile},
		{name: "migration plan", create: createNewPrivatePlanFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(privateOutputACLRoot(t), "private-output.json")
			const original = "existing private destination"
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}

			file, err := tc.create(path)
			if file != nil {
				_ = file.Close()
			}
			if err == nil {
				t.Fatal("creator replaced an existing destination")
			}
			contents, readErr := os.ReadFile(path)
			if readErr != nil || string(contents) != original {
				t.Fatalf("existing destination changed: %q %v", contents, readErr)
			}
		})
	}
}

func setDarwinInheritedReadACL(t *testing.T, path string) {
	t.Helper()
	command := exec.Command("chmod", "+a", "everyone allow read,file_inherit", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("Darwin inherited ACL fixture unavailable: %v: %s", err, output)
	}
}
