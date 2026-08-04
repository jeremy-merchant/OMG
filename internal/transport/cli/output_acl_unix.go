//go:build !windows

package cli

import (
	"github.com/jeremy-merchant/oh-my-group/internal/unixacl"
	"golang.org/x/sys/unix"
)

// rejectInheritedPayloadACL closes a newly created file and removes its leaf
// only when the parent directory still names that exact descriptor. This keeps
// ACL rejection fail-closed without ever removing a pre-existing replacement.
func rejectInheritedPayloadACL(parentFD, fd int, leaf string) error {
	if err := unixacl.RejectPayloadACLFD(fd); err != nil {
		var created unix.Stat_t
		statErr := unix.Fstat(fd, &created)
		_ = unix.Close(fd)
		if statErr == nil {
			var current unix.Stat_t
			if unix.Fstatat(parentFD, leaf, &current, unix.AT_SYMLINK_NOFOLLOW) == nil &&
				current.Dev == created.Dev && current.Ino == created.Ino {
				_ = unix.Unlinkat(parentFD, leaf, 0)
			}
		}
		return err
	}
	return nil
}
