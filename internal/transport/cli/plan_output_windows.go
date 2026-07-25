//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/jeremy-merchant/OMG/internal/platform"
	"github.com/jeremy-merchant/OMG/internal/windowsacl"

	"golang.org/x/sys/windows"
)

func createNewPrivatePlanFile(path string) (*os.File, error) {
	if err := platform.ValidateLocalWindowsPath(path); err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("plan output path must be absolute, clean, and local")
	}
	if err := validatePrivatePlanParent(path); err != nil {
		return nil, err
	}
	sid, err := planCurrentUserSID()
	if err != nil {
		return nil, err
	}
	descriptor, err := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;;FA;;;" + sid.String() + ")")
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, &attrs, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func validatePrivatePlanParent(path string) error {
	if err := platform.ValidateLocalWindowsPath(path); err != nil {
		return errors.New("plan output path must be local")
	}
	volume := filepath.VolumeName(path)
	current := volume + string(filepath.Separator)
	remaining := strings.TrimPrefix(path[len(volume):], string(filepath.Separator))
	components := strings.Split(remaining, string(filepath.Separator))
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			return errors.New("invalid plan output ancestor")
		}
		current = filepath.Join(current, component)
		attrs, err := planWindowsAttributes(current)
		if err != nil {
			return err
		}
		if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return errors.New("unsafe plan output ancestor")
		}
	}
	sid, err := planCurrentUserSID()
	if err != nil {
		return err
	}
	return validatePrivatePlanDACL(filepath.Dir(path), sid)
}

func planWindowsAttributes(path string) (uint32, error) {
	if err := platform.ValidateLocalWindowsPath(path); err != nil {
		return 0, errors.New("plan output path must be local")
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.GetFileAttributes(name)
}

func planCurrentUserSID() (*windows.SID, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, fmt.Errorf("current user token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid.Copy()
}

func validatePrivatePlanDACL(path string, sid *windows.SID) error {
	if err := platform.ValidateLocalWindowsPath(path); err != nil {
		return errors.New("plan output path must be local")
	}
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if !windowsacl.IsPrivate(descriptor, sid) {
		return errors.New("plan output parent DACL is not private")
	}
	return nil
}
