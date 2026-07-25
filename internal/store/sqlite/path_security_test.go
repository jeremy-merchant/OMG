package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSecuresManagedStateAndRejectsUnsafeFinalPath(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "state")
	path := filepath.Join(dir, "state.db")
	store, _, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	broad := filepath.Join(root, "broad.db")
	if err := os.WriteFile(broad, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(context.Background(), broad, OpenOptions{}); err == nil {
		t.Fatal("broad file accepted")
	}
	link := filepath.Join(root, "link.db")
	if err := os.Symlink(broad, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(context.Background(), link, OpenOptions{}); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestOpenRejectsSymlinkedStateParent(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "state")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(context.Background(), filepath.Join(link, "state.db"), OpenOptions{}); err == nil {
		t.Fatal("symlinked parent accepted")
	}
}
