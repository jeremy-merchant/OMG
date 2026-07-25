//go:build !windows

package cli

import (
	"errors"
	"os"

	"github.com/jeremy-merchant/OMG/internal/unixacl"
	"golang.org/x/sys/unix"
)

func readPrivatePayloadFile(path string) ([]byte, error) {
	parentFD, leaf, err := openPrivatePlanParent(path)
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
		return nil, errors.New("application payload file is unavailable")
	}
	defer file.Close()

	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0o077 != 0 || info.Uid != uint32(os.Geteuid()) || info.Size > maxApplicationPayload {
		return nil, errors.New("application payload file is not private and regular")
	}
	if err := unixacl.RejectPayloadACLFD(fd); err != nil {
		return nil, errors.New("application payload file extended ACL is not private")
	}
	return readBoundedPayload(file)
}
