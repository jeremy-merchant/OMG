package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathInspectorFreshDestinationRequiresAbsentLeafAndSafeExistingChain(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inspector := NewPathInspector()
	fresh := filepath.Join(root, "fresh.db")
	if !inspector.FreshDestination(fresh) {
		t.Fatal("fresh destination beneath an existing directory was rejected")
	}
	if err := os.WriteFile(fresh, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if inspector.FreshDestination(fresh) {
		t.Fatal("existing destination was accepted")
	}
	if inspector.FreshDestination(filepath.Join(root, "missing", "fresh.db")) {
		t.Fatal("destination beneath a missing parent was accepted")
	}
}

func TestPathInspectorFreshDestinationRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	if NewPathInspector().FreshDestination(filepath.Join(linkedParent, "fresh.db")) {
		t.Fatal("destination beneath a symlinked parent was accepted")
	}
}

func TestPathInspectorSameDirectoryUsesFilesystemIdentity(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	inspector := NewPathInspector()
	if !inspector.SameDirectory(alias, root) {
		t.Fatal("filesystem-identical directory alias was rejected")
	}
	if inspector.SameDirectory(alias+"/../"+filepath.Base(alias), root) {
		t.Fatal("non-canonical candidate spelling was accepted")
	}
	if inspector.SameDirectory(".", root) {
		t.Fatal("relative candidate was accepted")
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if inspector.SameDirectory(file, file) {
		t.Fatal("regular file was accepted as a directory")
	}
}
