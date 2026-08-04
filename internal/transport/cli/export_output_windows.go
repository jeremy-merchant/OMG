//go:build windows

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/jeremy-merchant/oh-my-group/internal/platform"
	"github.com/jeremy-merchant/oh-my-group/internal/windowsacl"

	"golang.org/x/sys/windows"
)

var queryExportSecurityInfo = windows.GetSecurityInfo

func createNewPrivateExportFile(path string) (*os.File, error) {
	if err := platform.ValidateLocalWindowsPath(path); err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("export path must be absolute, clean, and local")
	}
	if err := validateExportAncestors(path); err != nil {
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
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE|windows.DELETE, 0, &attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	descriptor, err = queryExportSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || !windowsacl.IsPrivate(descriptor, sid) {
		cleanupErr := removeCreatedExportFile(handle)
		closeErr := windows.CloseHandle(handle)
		if cleanupErr != nil {
			return nil, cleanupErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return nil, errors.New("export output DACL is not private")
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = removeCreatedExportFile(handle)
		_ = windows.CloseHandle(handle)
		return nil, errors.New("export output is unavailable")
	}
	return file, nil
}

func removeCreatedExportFile(handle windows.Handle) error {
	disposition := struct {
		DeleteFile uint32
	}{DeleteFile: 1}
	return windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo, (*byte)(unsafe.Pointer(&disposition)), uint32(unsafe.Sizeof(disposition)))
}

func validateExportAncestors(path string) error {
	if err := platform.ValidateLocalWindowsPath(path); err != nil {
		return errors.New("export path must be local")
	}
	volume := filepath.VolumeName(path)
	current := volume + string(filepath.Separator)
	remaining := strings.TrimPrefix(path[len(volume):], string(filepath.Separator))
	components := strings.Split(remaining, string(filepath.Separator))
	if len(components) < 2 {
		return errors.New("invalid export path")
	}
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			return errors.New("invalid export ancestor")
		}
		current = filepath.Join(current, component)
		name, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return err
		}
		attrs, err := windows.GetFileAttributes(name)
		if err != nil {
			return err
		}
		if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return errors.New("unsafe export ancestor")
		}
	}
	return nil
}
