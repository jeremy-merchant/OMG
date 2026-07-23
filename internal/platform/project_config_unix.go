//go:build !windows

package platform

import (
	"context"
	"errors"
	"os"

	"example.invalid/coordledger/internal/ports"
	"golang.org/x/sys/unix"
)

const initialProjectConfig = "# OMG project configuration\n"

type projectConfigInitializer struct{}

var _ ports.ProjectConfigInitializer = projectConfigInitializer{}

// NewProjectConfigInitializer provides the platform-bound filesystem and ACL
// implementation used by application composition.
func NewProjectConfigInitializer() ports.ProjectConfigInitializer { return projectConfigInitializer{} }

func (projectConfigInitializer) InitializeProjectConfig(_ context.Context, root string) error {
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	if err := unix.Mkdirat(rootFD, ".omg", 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	configFD, err := unix.Openat(rootFD, ".omg", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(configFD)
	if err := ensurePrivateOperatorDir(configFD); err != nil {
		return err
	}
	fd, err := unix.Openat(configFD, "project.toml", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if errors.Is(err, unix.EEXIST) {
		existing, openErr := unix.Openat(configFD, "project.toml", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			return openErr
		}
		defer unix.Close(existing)
		var info unix.Stat_t
		if err := unix.Fstat(existing, &info); err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG {
			return errors.New("project configuration is not regular")
		}
		return nil
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "project.toml")
	if file == nil {
		unix.Close(fd)
		return errors.New("project configuration is unavailable")
	}
	defer file.Close()
	if _, err := file.WriteString(initialProjectConfig); err != nil {
		return err
	}
	return file.Sync()
}

func ensurePrivateOperatorDir(configFD int) error {
	if err := unix.Mkdirat(configFD, "private", 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	fd, err := unix.Openat(configFD, "private", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Mode&0o077 != 0 || info.Uid != uint32(os.Geteuid()) {
		return errors.New("private operator directory is not owner-only")
	}
	return nil
}
