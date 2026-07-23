//go:build windows

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCreateNewPrivateExportFileRejectsBroadHandleDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.html")
	sid, err := planCurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	broad, err := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;;FA;;;" + sid.String() + ")(A;;GR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	original := queryExportSecurityInfo
	queryExportSecurityInfo = func(windows.Handle, windows.SE_OBJECT_TYPE, windows.SECURITY_INFORMATION) (*windows.SECURITY_DESCRIPTOR, error) {
		return broad, nil
	}
	t.Cleanup(func() { queryExportSecurityInfo = original })

	file, err := createNewPrivateExportFile(path)
	if err == nil {
		if file != nil {
			_ = file.Close()
		}
		t.Fatal("createNewPrivateExportFile accepted broad handle DACL")
	}
	if file != nil {
		t.Fatal("createNewPrivateExportFile returned a file after rejecting its DACL")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected export remains at %q: %v", path, err)
	}
}

func TestCreateNewPrivateExportFileRejectsUnsupportedHandleACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.html")
	original := queryExportSecurityInfo
	queryExportSecurityInfo = func(windows.Handle, windows.SE_OBJECT_TYPE, windows.SECURITY_INFORMATION) (*windows.SECURITY_DESCRIPTOR, error) {
		return nil, windows.ERROR_NOT_SUPPORTED
	}
	t.Cleanup(func() { queryExportSecurityInfo = original })

	file, err := createNewPrivateExportFile(path)
	if err == nil {
		if file != nil {
			_ = file.Close()
		}
		t.Fatal("createNewPrivateExportFile accepted unsupported handle ACL query")
	}
	if file != nil {
		t.Fatal("createNewPrivateExportFile returned a file after unsupported ACL query")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected export remains at %q: %v", path, err)
	}
}

func TestCreateNewPrivateExportFileAcceptsPrivateHandleDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.html")
	file, err := createNewPrivateExportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("private")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	assertPrivateExportFile(t, path)
}
