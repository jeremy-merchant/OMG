//go:build windows

package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jeremy-merchant/OMG/internal/platform"

	"golang.org/x/sys/windows"
)

func readPrivatePayloadFile(path string) ([]byte, error) {
	if err := platform.ValidateLocalWindowsPath(path); err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("application payload path must be absolute, clean, and local")
	}
	if err := validatePrivatePlanParent(path); err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		windows.CloseHandle(handle)
		return nil, errors.New("application payload file is unavailable")
	}
	defer file.Close()

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, err
	}
	size := uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow)
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 || size > maxApplicationPayload {
		return nil, errors.New("application payload file is not private and regular")
	}
	sid, err := planCurrentUserSID()
	if err != nil {
		return nil, err
	}
	if err := validatePrivatePlanDACL(path, sid); err != nil {
		return nil, err
	}
	return readBoundedPayload(file)
}
