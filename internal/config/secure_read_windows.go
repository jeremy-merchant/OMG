//go:build windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jeremy-merchant/oh-my-group/internal/windowsacl"
	"golang.org/x/sys/windows"
)

func readConfigFile(path string, private bool) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("project configuration path must be absolute and clean")
	}
	if err := validateConfigAncestors(path); err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		windows.CloseHandle(handle)
		return nil, errors.New("project configuration is unavailable")
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, err
	}
	size := uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow)
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 || size > maxConfigBytes {
		return nil, errors.New("project configuration is not a bounded regular file")
	}
	if private {
		sid, err := configCurrentUserSID()
		if err != nil {
			return nil, err
		}
		descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			return nil, err
		}
		if !windowsacl.IsPrivate(descriptor, sid) {
			return nil, errors.New("local project configuration is not owner-only")
		}
	}
	return readBoundedConfig(file)
}

func validateConfigAncestors(path string) error {
	volume := filepath.VolumeName(path)
	current := volume + string(filepath.Separator)
	remaining := strings.TrimPrefix(path[len(volume):], string(filepath.Separator))
	components := strings.Split(remaining, string(filepath.Separator))
	if len(components) < 2 {
		return errors.New("invalid project configuration path")
	}
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			return errors.New("invalid project configuration ancestor")
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
			return errors.New("unsafe project configuration ancestor")
		}
	}
	return nil
}

func configCurrentUserSID() (*windows.SID, error) {
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
