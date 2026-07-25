//go:build !windows

package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSecuresManagedStateWithPrivatePOSIXModes(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "state")
	path := filepath.Join(dir, "state.db")
	store, _, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, want := range []struct {
		path string
		mode os.FileMode
	}{{dir, 0o700}, {path, 0o600}} {
		info, statErr := os.Stat(want.path)
		if statErr != nil || info.Mode().Perm() != want.mode {
			t.Fatalf("%s mode=%#o err=%v", want.path, info.Mode().Perm(), statErr)
		}
	}
}
