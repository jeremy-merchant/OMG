//go:build darwin

package sqlite

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSecureStatePathRejectsExtendedWritableAncestorACL(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	addDarwinStateACL(t, parent, "everyone allow write")

	if err := secureStatePath(filepath.Join(parent, "state.db"), false); err == nil {
		t.Fatal("0700 state ancestor with an extended everyone-write ACL was accepted")
	}
}

func TestOpenRejects0600ArtifactWithDangerousExtendedACL(t *testing.T) {
	path := filepath.Join(canonicalTempDir(t), "state.db")
	store, _, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	addDarwinStateACL(t, path, "everyone allow write")

	store, _, err = Open(context.Background(), path, OpenOptions{ReadOnly: true, ExistingOnly: true})
	if store != nil {
		store.Close()
	}
	if err == nil {
		t.Fatal("0600 state artifact with an extended everyone-write ACL was opened")
	}
}

func TestOpenAllows0600ArtifactWithoutExtendedACL(t *testing.T) {
	path := filepath.Join(canonicalTempDir(t), "state.db")
	store, _, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	store, _, err = Open(context.Background(), path, OpenOptions{ReadOnly: true, ExistingOnly: true})
	if err != nil {
		t.Fatalf("safe 0600 state artifact rejected: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSecureStatePathRejectsDangerousExtendedACLOnEveryArtifact(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(canonicalTempDir(t), "state.db")
			artifact := path + suffix
			if err := os.WriteFile(artifact, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			addDarwinStateACL(t, artifact, "everyone allow write")

			if err := secureStatePath(path, false); err == nil {
				t.Fatalf("0600 %q artifact with extended everyone-write ACL was accepted", suffix)
			}
		})
	}
}

func addDarwinStateACL(t *testing.T, path, entry string) {
	t.Helper()
	command := exec.Command("chmod", "+a", entry, path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("Darwin ACL fixture unavailable: %v: %s", err, output)
	}
}
