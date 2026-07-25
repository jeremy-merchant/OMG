//go:build windows

package sqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jeremy-merchant/OMG/internal/platform"

	"github.com/jeremy-merchant/OMG/internal/windowsacl"

	"golang.org/x/sys/windows"
)

// secureStatePath accepts only a canonical local path under a private,
// protected-DACL directory. Existing objects are validated, never repaired.
func secureStatePath(path string, createParent bool) error {
	return secureStatePathWithArtifactProbe(path, createParent, stateArtifactsAbsent)
}

// secureStatePathWithArtifactProbe keeps ancestor validation ahead of every
// state-artifact probe. The probe parameter makes that security boundary
// directly testable without accessing an artifact through a reparse point.
func secureStatePathWithArtifactProbe(path string, createParent bool, artifactProbe func(string) (bool, error)) error {
	if err := platform.ValidateLocalWindowsPath(path); err != nil || path == filepath.VolumeName(path)+string(filepath.Separator) {
		return errors.New("sqlite: path must be absolute, clean, and local")
	}

	sid, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("sqlite: current user SID: %w", err)
	}
	parent := filepath.Dir(path)
	if err := validateExistingNoReparseAncestors(parent); err != nil {
		return err
	}
	fresh, err := artifactProbe(path)
	if err != nil {
		return err
	}
	if err := secureStateDirectory(parent, createParent, createParent && fresh, sid); err != nil {
		if !createParent && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, candidate := range sqliteArtifacts(path) {
		exists, err := pathExists(candidate)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := validateWindowsPath(candidate, false); err != nil {
			return err
		}
		if err := validatePrivateDACL(candidate, sid); err != nil {
			return err
		}
	}
	return nil
}

// secureStateArtifacts verifies the managed directory and applies the private
// descriptor only to artifacts SQLite created after secureStatePath returned.
func secureStateArtifacts(path string) error {
	if err := platform.ValidateLocalWindowsPath(path); err != nil {
		return errors.New("sqlite: path must be absolute, clean, and local")
	}
	sid, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("sqlite: current user SID: %w", err)
	}
	if err := secureStateDirectory(filepath.Dir(path), false, false, sid); err != nil {
		return err
	}
	for _, candidate := range sqliteArtifacts(path) {
		exists, err := pathExists(candidate)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := validateWindowsPath(candidate, false); err != nil {
			return err
		}
		if err := applyPrivateDACL(candidate, sid); err != nil {
			return err
		}
		if err := validatePrivateDACL(candidate, sid); err != nil {
			return err
		}
	}
	return nil
}

func stateArtifactsAbsent(path string) (bool, error) {
	for _, candidate := range sqliteArtifacts(path) {
		exists, err := pathExists(candidate)
		if err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
	}
	return true, nil
}

func currentUserSID() (*windows.SID, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid.Copy()
}

func secureStateDirectory(path string, create, initializeExisting bool, sid *windows.SID) error {
	if err := platform.ValidateLocalWindowsPath(path); err != nil {
		return errors.New("sqlite: state directory path must be local")
	}
	if err := validateExistingNoReparseAncestors(path); err != nil {
		return err
	}
	exists, err := pathExists(path)
	if err != nil {
		return err
	}
	if exists {
		if err := validateWindowsPath(path, true); err != nil {
			return err
		}
		if !initializeExisting {
			return validatePrivateDACL(path, sid)
		}
		if err := validateCurrentOwner(path, sid); err != nil {
			return err
		}
		if err := applyPrivateDACL(path, sid); err != nil {
			return err
		}
		return validatePrivateDACL(path, sid)
	}
	if !create {
		return os.ErrNotExist
	}
	if err := createMissingStateDirectories(path); err != nil {
		return err
	}
	if err := applyPrivateDACL(path, sid); err != nil {
		return err
	}
	return validatePrivateDACL(path, sid)
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func validateWindowsPath(path string, directory bool) error {
	attrs, err := windowsFileAttributes(path)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("sqlite: reparse points are not permitted in state paths")
	}
	if directory != (attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0) {
		return errors.New("sqlite: unsafe state path kind")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if directory {
		if !info.IsDir() {
			return errors.New("sqlite: unsafe state path kind")
		}
	} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("sqlite: unsafe state path")
	}
	return nil
}

func validateExistingNoReparseAncestors(path string) error {
	volume := filepath.VolumeName(path)
	remaining := strings.TrimPrefix(path[len(volume):], string(filepath.Separator))
	current := volume + string(filepath.Separator)
	for _, component := range strings.Split(remaining, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		exists, err := pathExists(current)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		if err := validateWindowsPath(current, true); err != nil {
			return err
		}
	}
	return nil
}

func createMissingStateDirectories(path string) error {
	volume := filepath.VolumeName(path)
	remaining := strings.TrimPrefix(path[len(volume):], string(filepath.Separator))
	current := volume + string(filepath.Separator)
	for _, component := range strings.Split(remaining, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		exists, err := pathExists(current)
		if err != nil {
			return err
		}
		if !exists {
			if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
		}
		if err := validateWindowsPath(current, true); err != nil {
			return err
		}
	}
	return nil
}

func windowsFileAttributes(path string) (uint32, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.GetFileAttributes(name)
}

func privateDescriptor(sid *windows.SID) (*windows.SECURITY_DESCRIPTOR, error) {
	return windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;;FA;;;" + sid.String() + ")")
}

func applyPrivateDACL(path string, sid *windows.SID) error {
	descriptor, err := privateDescriptor(sid)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		sid, nil, dacl, nil)
}

func validateCurrentOwner(path string, sid *windows.SID) error {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !owner.Equals(sid) {
		return errors.New("sqlite: state path owner is not the current user")
	}
	return nil
}

func validatePrivateDACL(path string, sid *windows.SID) error {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if !windowsacl.IsPrivate(descriptor, sid) {
		return errors.New("sqlite: state path DACL is not private")
	}
	return nil
}
