//go:build darwin

package unixacl

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRejectPayloadACLFDAllowsFileWithoutExtendedACL(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "private")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := RejectPayloadACLFD(int(file.Fd())); err != nil {
		t.Fatalf("ordinary file rejected: %v", err)
	}
	if err := unix.Fchmod(int(file.Fd()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParseDarwinUUIDRejectsMalformedOutput(t *testing.T) {
	if _, err := parseDarwinUUID("not-a-uuid"); err == nil {
		t.Fatal("malformed UUID accepted")
	}
	guid, err := parseDarwinUUID("11177B59-8CC9-4F9E-A384-36E550C30F8F")
	want := [16]byte{0x59, 0x7b, 0x17, 0x11, 0xc9, 0x8c, 0x9e, 0x4f, 0xa3, 0x84, 0x36, 0xe5, 0x50, 0xc3, 0x0f, 0x8f}
	if err != nil || guid != want {
		t.Fatalf("UUID = %x, %v; want %x", guid, err, want)
	}
}

func TestRejectPayloadACLFDAllowsDenyOnlyACL(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "private")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if output, err := exec.Command("chmod", "+a", "everyone deny read", file.Name()).CombinedOutput(); err != nil {
		t.Skipf("Darwin ACL fixture unavailable: %v: %s", err, output)
	}
	if err := RejectPayloadACLFD(int(file.Fd())); err != nil {
		t.Fatalf("deny-only ACL rejected: %v", err)
	}
}

func TestRejectStateAncestorACLFDAllowsHarmlessPermitACL(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "private")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if output, err := exec.Command("chmod", "+a", "everyone allow read", file.Name()).CombinedOutput(); err != nil {
		t.Skipf("Darwin ACL fixture unavailable: %v: %s", err, output)
	}
	if err := RejectStateAncestorACLFD(int(file.Fd())); err != nil {
		t.Fatalf("harmless permit ACL rejected: %v", err)
	}
}

func TestRejectPayloadACLFDAllowsOwnerDangerousPermitACL(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "private")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	username, err := exec.Command("id", "-un").Output()
	if err != nil {
		t.Fatal(err)
	}
	entry := "user:" + strings.TrimSpace(string(username)) + " allow read"
	if output, err := exec.Command("chmod", "+a", entry, file.Name()).CombinedOutput(); err != nil {
		t.Skipf("Darwin ACL fixture unavailable: %v: %s", err, output)
	}
	if err := RejectPayloadACLFD(int(file.Fd())); err != nil {
		t.Fatalf("owner permit ACL rejected: %v", err)
	}
}
