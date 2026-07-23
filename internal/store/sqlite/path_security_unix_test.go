//go:build !windows

package sqlite

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestUnixOwnerMatchesCurrentUser(t *testing.T) {
	if !unixOwnerMatches(42, 42) {
		t.Fatal("matching UID rejected")
	}
	if unixOwnerMatches(42, 43) {
		t.Fatal("foreign UID accepted")
	}
}

func TestUnixSecureStatePathRejectsIntermediateSymlinkForExistingManagedDirectory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	managed := filepath.Join(target, "managed")
	if err := os.MkdirAll(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "intermediate")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := secureStatePath(filepath.Join(link, "managed", "state.db"), false); err == nil {
		t.Fatal("existing managed directory under intermediate symlink accepted")
	}
}

func TestUnixSecureStatePathRejectsSpecialAncestor(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "omg-path-security-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "state.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "state.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	for _, path := range []string{fifo, socket, unixDevicePath(t)} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if err := secureStatePath(filepath.Join(path, "state.db"), false); err == nil {
				t.Fatalf("special ancestor %q accepted", path)
			}
		})
	}
}

func TestUnixSecureStatePathAcceptsOrdinaryTempAncestor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed", "state.db")
	if err := secureStatePath(path, true); err != nil {
		t.Fatalf("ordinary temp ancestor rejected: %v", err)
	}
}

func TestUnixSecureStatePathAcceptsHomeRootAncestorWithoutCreatingState(t *testing.T) {
	if info, err := os.Stat("/Users"); err != nil || !info.IsDir() {
		t.Skip("/Users is unavailable on this platform")
	}
	path := filepath.Join("/Users", ".omg-acl-ancestor-test", "state.db")
	if err := secureStatePath(path, false); err != nil {
		t.Fatalf("ordinary home-root ancestor rejected: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("existing-only validation created state parent: %v", err)
	}
}

func TestUnixSecureStatePathRejectsWritableAncestor(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	if err := secureStatePath(filepath.Join(root, "managed", "state.db"), true); err == nil {
		t.Fatal("state path under writable ancestor accepted")
	}
}

func TestOpenRejectsExistingMutatingStateUnderWritableAncestor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state", "state.db")
	store, _, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	if _, _, err := Open(context.Background(), path, OpenOptions{ExistingOnly: true}); err == nil {
		t.Fatal("existing mutating state under writable ancestor accepted")
	}
}

func unixDevicePath(t *testing.T) string {
	t.Helper()
	info, err := os.Lstat("/dev/null")
	if err != nil || info.Mode()&os.ModeDevice == 0 {
		t.Skip("/dev/null is not a device on this platform")
	}
	return "/dev/null"
}
