//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadPrivatePayloadFileWindowsSecurityBoundary(t *testing.T) {
	sid, err := planCurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	setPayloadDACL(t, parent, "O:"+sid.String()+"D:P(A;;FA;;;"+sid.String()+")")

	payload := []byte(`{"raw_token":"transport-only"}`)
	valid := filepath.Join(parent, "payload.json")
	if err := os.WriteFile(valid, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	setPayloadDACL(t, valid, "O:"+sid.String()+"D:P(A;;FA;;;"+sid.String()+")")
	got, err := readPrivatePayloadFile(valid)
	if err != nil {
		t.Fatalf("valid private payload: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload = %q; want %q", got, payload)
	}

	broad := filepath.Join(parent, "broad.json")
	if err := os.WriteFile(broad, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	setPayloadDACL(t, broad, "O:"+sid.String()+"D:P(A;;FA;;;WD)")
	for name, path := range map[string]string{"relative": filepath.Base(valid), "broad": broad, "directory": parent} {
		t.Run(name, func(t *testing.T) {
			if _, err := readPrivatePayloadFile(path); err == nil {
				t.Fatalf("unsafe payload path %q was accepted", path)
			}
		})
	}

	symlink := filepath.Join(parent, "symlink.json")
	if err := os.Symlink(valid, symlink); err == nil {
		if _, err := readPrivatePayloadFile(symlink); err == nil {
			t.Fatal("reparse-point payload was accepted")
		}
	}
}

func setPayloadDACL(t *testing.T, path, sddl string) {
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
