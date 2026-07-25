//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/jeremy-merchant/OMG/internal/ports"
	"github.com/jeremy-merchant/OMG/internal/windowsacl"
	"golang.org/x/sys/windows"
)

const initialProjectConfig = "# OMG project configuration\n"

type projectConfigInitializer struct{}

var _ ports.ProjectConfigInitializer = projectConfigInitializer{}

func NewProjectConfigInitializer() ports.ProjectConfigInitializer { return projectConfigInitializer{} }

func (projectConfigInitializer) InitializeProjectConfig(_ context.Context, root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("project root must be absolute and clean")
	}
	attrs, err := privateSecurityAttributes()
	if err != nil {
		return err
	}
	directory := filepath.Join(root, ".omg")
	name, err := windows.UTF16PtrFromString(directory)
	if err != nil {
		return err
	}
	if err := windows.CreateDirectory(name, attrs); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	if err := validatePrivateDirectory(directory); err != nil {
		return err
	}
	if err := ensurePrivateOperatorDirectory(directory, attrs); err != nil {
		return err
	}
	path := filepath.Join(directory, "project.toml")
	fileName, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(fileName, windows.GENERIC_WRITE, 0, attrs, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return validatePrivateFile(path)
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		windows.CloseHandle(handle)
		return errors.New("project configuration is unavailable")
	}
	defer file.Close()
	if _, err := file.WriteString(initialProjectConfig); err != nil {
		return err
	}
	return file.Sync()
}

func ensurePrivateOperatorDirectory(configDirectory string, attrs *windows.SecurityAttributes) error {
	path := filepath.Join(configDirectory, "private")
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := windows.CreateDirectory(name, attrs); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	return validatePrivateDirectory(path)
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

func privateSecurityAttributes() (*windows.SecurityAttributes, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	descriptor, err := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;;FA;;;" + sid.String() + ")")
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}, nil
}

func validatePrivateDirectory(path string) error {
	attrs, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(path))
	if err != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return errors.New("unsafe project configuration directory")
	}
	return validatePrivateDACL(path)
}

func validatePrivateFile(path string) error {
	attrs, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(path))
	if err != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return errors.New("unsafe project configuration file")
	}
	return validatePrivateDACL(path)
}

func validatePrivateDACL(path string) error {
	sid, err := currentUserSID()
	if err != nil {
		return err
	}
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if !windowsacl.IsPrivate(descriptor, sid) {
		return fmt.Errorf("project configuration is not private")
	}
	return nil
}
