//go:build windows

package sqlite

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsStatePathsRejectNonLocalNamespacesBeforeStateAccess(t *testing.T) {
	for _, path := range []string{
		`\\server\share\state.db`,
		`\\?\UNC\server\share\state.db`,
		`\\?\C:\state.db`,
		`\\.\PhysicalDrive0`,
		`\??\C:\state.db`,
	} {
		if err := secureStatePath(path, true); err == nil {
			t.Fatalf("non-local state path %q was accepted", path)
		}
		if err := secureStateArtifacts(path); err == nil {
			t.Fatalf("non-local state artifact path %q was accepted", path)
		}
	}
}

func TestWindowsSecureStatePathCreatesPrivateDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "state.db")
	if err := secureStatePath(path, true); err != nil {
		t.Fatal(err)
	}
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateDACL(filepath.Dir(path), sid); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := secureStateArtifacts(path); err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateDACL(path, sid); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsSecureStatePathRejectsReparseAncestorForExistingManagedDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	managed := filepath.Join(target, "managed")
	if err := os.MkdirAll(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	reparse := filepath.Join(root, "reparse")
	if err := os.Symlink(target, reparse); err != nil {
		t.Skipf("cannot create directory reparse point: %v", err)
	}

	if err := secureStatePath(filepath.Join(reparse, "managed", "state.db"), false); err == nil {
		t.Fatal("existing managed directory beneath reparse ancestor accepted")
	}
}

func TestWindowsSecureStatePathRejectsReparseAncestorBeforeArtifactProbe(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	reparse := filepath.Join(root, "reparse")
	if err := os.Symlink(target, reparse); err != nil {
		t.Skipf("cannot create directory reparse point: %v", err)
	}

	probes := 0
	err := secureStatePathWithArtifactProbe(filepath.Join(reparse, "state.db"), true, func(string) (bool, error) {
		probes++
		return true, nil
	})
	if err == nil {
		t.Fatal("state path beneath reparse ancestor accepted")
	}
	if probes != 0 {
		t.Fatalf("artifact probe ran %d times before reparse ancestor was rejected", probes)
	}
}

func TestWindowsSecureStatePathRejectsReparseAncestorBeforeCreatingMissingDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	reparse := filepath.Join(root, "reparse")
	if err := os.Symlink(target, reparse); err != nil {
		t.Skipf("cannot create directory reparse point: %v", err)
	}

	redirectedDirectory := filepath.Join(target, "created-through-reparse")
	if err := secureStatePath(filepath.Join(reparse, "created-through-reparse", "state.db"), true); err == nil {
		t.Fatal("missing managed directory beneath reparse ancestor accepted")
	}
	if _, err := os.Lstat(redirectedDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reparse ancestor created redirected directory %q: %v", redirectedDirectory, err)
	}
}

func TestWindowsExistingOnlyDoesNotRewriteSharedCurrentUserDirectoryDACL(t *testing.T) {
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	setWindowsDACL(t, parent, "O:"+sid.String()+"D:P(A;;FA;;;"+sid.String()+")(A;;0x1200a9;;;WD)")
	before := windowsSecurityDescriptorBytes(t, parent)

	err = secureStatePath(filepath.Join(parent, "absent.db"), false)
	if err == nil {
		t.Fatal("existing-only path accepted shared parent DACL")
	}
	after := windowsSecurityDescriptorBytes(t, parent)
	if !bytes.Equal(after, before) {
		t.Fatal("existing-only path rewrote shared parent security descriptor")
	}
}

func TestWindowsExistingOnlyOpenDoesNotRewriteExistingArtifactDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "state.db")
	store, _, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	setWindowsDACL(t, path, "O:"+sid.String()+"D:(A;;FA;;;"+sid.String()+")")
	before := windowsSecurityDescriptorBytes(t, path)

	store, _, err = Open(context.Background(), path, OpenOptions{ExistingOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	after := windowsSecurityDescriptorBytes(t, path)
	if !bytes.Equal(after, before) {
		t.Fatal("existing-only open rewrote existing artifact security descriptor")
	}
}

func TestWindowsExistingOnlyMissingParentDoesNotCreateState(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "missing")
	if err := secureStatePath(filepath.Join(parent, "state.db"), false); err != nil {
		t.Fatalf("existing-only missing state: %v", err)
	}
	if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("existing-only path created missing parent %q: %v", parent, err)
	}
}

func setWindowsDACL(t *testing.T, path, sddl string) {
	t.Helper()
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func windowsSecurityDescriptorBytes(t *testing.T, path string) []byte {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(descriptor)), int(descriptor.Length()))...)
}
