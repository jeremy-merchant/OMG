//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadPrivatePayloadFileUnixSecurityBoundary(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "private")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"raw_token":"transport-only"}`)
	valid := filepath.Join(parent, "payload.json")
	if err := os.WriteFile(valid, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readPrivatePayloadFile(valid)
	if err != nil {
		t.Fatalf("valid private payload: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload = %q; want %q", got, payload)
	}

	broad := filepath.Join(parent, "broad.json")
	if err := os.WriteFile(broad, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(parent, "symlink.json")
	if err := os.Symlink(valid, symlink); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(parent, "payload.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"relative":  filepath.Base(valid),
		"broad":     broad,
		"symlink":   symlink,
		"directory": parent,
		"fifo":      fifo,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readPrivatePayloadFile(path); err == nil {
				t.Fatalf("unsafe payload path %q was accepted", path)
			}
		})
	}
}
