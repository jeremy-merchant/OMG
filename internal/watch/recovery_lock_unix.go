//go:build !windows

package watch

import (
	"os"

	"golang.org/x/sys/unix"
)

func acquireRecoveryGuard(path string) (func(), bool) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, false
	}
	closeFD := func() { _ = unix.Close(fd) }

	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0o077 != 0 || info.Uid != uint32(os.Geteuid()) {
		closeFD()
		return nil, false
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		closeFD()
		return nil, false
	}
	return func() {
		_ = unix.Flock(fd, unix.LOCK_UN)
		closeFD()
	}, true
}
