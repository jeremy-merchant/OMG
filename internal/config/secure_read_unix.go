//go:build !windows

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func readConfigFile(path string, private bool) ([]byte, error) {
	parentFD, leaf, err := openConfigParent(path)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parentFD)
	fd, err := unix.Openat(parentFD, leaf, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		unix.Close(fd)
		return nil, errors.New("project configuration is unavailable")
	}
	defer file.Close()
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Size > maxConfigBytes {
		return nil, errors.New("project configuration is not a bounded regular file")
	}
	if private && (info.Mode&0o077 != 0 || info.Uid != uint32(os.Geteuid())) {
		return nil, errors.New("local project configuration is not owner-only")
	}
	return readBoundedConfig(file)
}

func openConfigParent(path string) (int, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return -1, "", errors.New("project configuration path must be absolute and clean")
	}
	components := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	if len(components) < 2 || components[len(components)-1] == "" {
		return -1, "", errors.New("invalid project configuration path")
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return -1, "", err
	}
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			unix.Close(fd)
			return -1, "", errors.New("invalid project configuration ancestor")
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
