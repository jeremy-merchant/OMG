//go:build windows

package platform

import (
	"errors"
	"testing"
)

func TestValidateLocalWindowsPathRejectsRemoteAndDevicePathsBeforeVolumeProbe(t *testing.T) {
	original := windowsDriveType
	defer func() { windowsDriveType = original }()
	probes := 0
	windowsDriveType = func(string) uint32 {
		probes++
		return windowsDriveFixed
	}

	for _, path := range []string{
		`\\server\share\state.db`,
		`\\?\UNC\server\share\state.db`,
		`\\?\C:\state.db`,
		`\\.\PhysicalDrive0`,
		`\??\C:\state.db`,
		`\state.db`,
		`C:state.db`,
	} {
		if err := ValidateLocalWindowsPath(path); !errors.Is(err, ErrNonLocalWindowsPath) {
			t.Fatalf("ValidateLocalWindowsPath(%q) error = %v; want non-local path", path, err)
		}
	}
	if probes != 0 {
		t.Fatalf("remote/device paths reached volume probe %d times", probes)
	}
}

func TestValidateLocalWindowsPathAcceptsCanonicalLocalDrivesAndRejectsRemoteVolumes(t *testing.T) {
	original := windowsDriveType
	defer func() { windowsDriveType = original }()

	for _, path := range []string{`C:\Users\operator\state.db`, `c:\Users\operator\state.db`} {
		windowsDriveType = func(string) uint32 { return windowsDriveFixed }
		if err := ValidateLocalWindowsPath(path); err != nil {
			t.Fatalf("ValidateLocalWindowsPath(%q): %v", path, err)
		}
	}

	windowsDriveType = func(string) uint32 { return windowsDriveRemote }
	if err := ValidateLocalWindowsPath(`Z:\share\state.db`); !errors.Is(err, ErrNonLocalWindowsPath) {
		t.Fatalf("remote volume error = %v; want non-local path", err)
	}
}

func TestValidateLocalWindowsPathRejectsAlternateStreamsAndNonCanonicalComponentsBeforeVolumeProbe(t *testing.T) {
	original := windowsDriveType
	defer func() { windowsDriveType = original }()
	probes := 0
	windowsDriveType = func(string) uint32 {
		probes++
		return windowsDriveFixed
	}

	for _, test := range []struct {
		path       string
		wantError  bool
		wantProbes int
	}{
		{path: `C:\state.db:metadata`, wantError: true},
		{path: `C:\state:metadata\state.db`, wantError: true},
		{path: `C:\state.`, wantError: true},
		{path: `C:\state `, wantError: true},
		{path: `C:\state?.db`, wantError: true},
		{path: "C:\\state\x00.db", wantError: true},
		{path: `C:\CON`, wantError: true},
		{path: `C:\prn.txt`, wantError: true},
		{path: `C:\AUX`, wantError: true},
		{path: `C:\nul.state`, wantError: true},
		{path: `C:\clock$.log`, wantError: true},
		{path: `C:\COM1`, wantError: true},
		{path: `C:\com9.txt`, wantError: true},
		{path: `C:\LPT1`, wantError: true},
		{path: `C:\lpt9.txt`, wantError: true},
		{path: `C:\COM¹`, wantError: true},
		{path: `C:\com².txt`, wantError: true},
		{path: `C:\COM³.log`, wantError: true},
		{path: `C:\LPT¹`, wantError: true},
		{path: `C:\lpt².txt`, wantError: true},
		{path: `C:\LPT³.log`, wantError: true},
		{path: `C:\CONN`, wantProbes: 1},
		{path: `C:\COM0`, wantProbes: 1},
		{path: `C:\COM10`, wantProbes: 1},
		{path: `C:\CLOCKS`, wantProbes: 1},
		{path: `C:\COM⁴`, wantProbes: 1},
		{path: `C:\LPT⁰`, wantProbes: 1},
		{path: `C:\Users\operator\state.db`, wantProbes: 1},
	} {
		before := probes
		err := ValidateLocalWindowsPath(test.path)
		if test.wantError {
			if !errors.Is(err, ErrNonLocalWindowsPath) {
				t.Errorf("ValidateLocalWindowsPath(%q) error = %v; want non-local path", test.path, err)
			}
		} else if err != nil {
			t.Errorf("ValidateLocalWindowsPath(%q): %v", test.path, err)
		}
		if got := probes - before; got != test.wantProbes {
			t.Errorf("ValidateLocalWindowsPath(%q) volume probes = %d; want %d", test.path, got, test.wantProbes)
		}
	}
}
