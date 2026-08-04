//go:build windows

package watch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jeremy-merchant/oh-my-group/internal/windowsacl"

	"golang.org/x/sys/windows"
)

func validateStateDir(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == filepath.VolumeName(path)+string(filepath.Separator) {
		return ErrInvalidConfig
	}
	if err := validateNoReparseAncestors(path); err != nil {
		return ErrInvalidConfig
	}
	if err := validateWindowsPath(path, true); err != nil {
		return ErrInvalidConfig
	}
	sid, err := currentUserSID()
	if err != nil || validatePrivateDACL(path, sid) != nil {
		return ErrInvalidConfig
	}
	return nil
}

func validatePrivateRegularFile(path string, info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false
	}
	if err := validateNoReparseAncestors(path); err != nil || validateWindowsPath(path, false) != nil {
		return false
	}
	sid, err := currentUserSID()
	return err == nil && validatePrivateDACL(path, sid) == nil
}

func secureNewPrivateFile(path string) error {
	if err := validateNoReparseAncestors(path); err != nil {
		return err
	}
	if err := validateWindowsPath(path, false); err != nil {
		return err
	}
	sid, err := currentUserSID()
	if err != nil {
		return err
	}
	if err := applyPrivateDACL(path, sid); err != nil {
		return err
	}
	return validatePrivateDACL(path, sid)
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

func validateWindowsPath(path string, directory bool) error {
	attrs, err := windowsFileAttributes(path)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || directory != (attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0) {
		return errors.New("unsafe watch state path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if directory {
		if !info.IsDir() {
			return errors.New("unsafe watch state path")
		}
	} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe watch state path")
	}
	return nil
}

func validateNoReparseAncestors(path string) error {
	volume := filepath.VolumeName(path)
	remaining := strings.TrimPrefix(path[len(volume):], string(filepath.Separator))
	current := volume + string(filepath.Separator)
	for _, component := range strings.Split(remaining, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		attrs, err := windowsFileAttributes(current)
		if err != nil {
			return err
		}
		if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("watch state paths cannot traverse reparse points")
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

func validatePrivateDACL(path string, sid *windows.SID) error {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if !windowsacl.IsPrivate(descriptor, sid) {
		return errors.New("watch state path DACL is not private")
	}
	return nil
}
