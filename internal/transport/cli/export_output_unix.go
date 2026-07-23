//go:build !windows

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func createNewPrivateExportFile(path string) (*os.File, error) {
	parentFD, leaf, err := openExportParent(path)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parentFD)
	fd, err := unix.Openat(parentFD, leaf, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := rejectInheritedPayloadACL(parentFD, fd, leaf); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openExportParent(path string) (int, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return -1, "", errors.New("export path must be absolute and clean")
	}
	components := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	if len(components) < 2 || components[len(components)-1] == "" {
		return -1, "", errors.New("invalid export path")
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return -1, "", err
	}
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			unix.Close(fd)
			return -1, "", errors.New("invalid export ancestor")
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if openErr != nil {
			return -1, "", openErr
		}
		fd = next
	}
	return fd, components[len(components)-1], nil
}
