//go:build darwin

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReadPrivatePayloadFileRejectsExtendedReadACL(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "private")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "payload.json")
	if err := os.WriteFile(path, []byte(`{"raw_token":"transport-only"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	addDarwinACL(t, path, "everyone allow read")

	if _, err := readPrivatePayloadFile(path); err == nil {
		t.Fatal("0600 payload with an extended everyone-read ACL was accepted")
	}
}

func addDarwinACL(t *testing.T, path, entry string) {
	t.Helper()
	command := exec.Command("chmod", "+a", entry, path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("Darwin ACL fixture unavailable: %v: %s", err, output)
	}
}
