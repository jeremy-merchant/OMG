//go:build !windows

package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/jeremy-merchant/oh-my-group/internal/unixacl"
	"golang.org/x/sys/unix"
)

// secureStatePath validates an absolute, canonical local state or backup path
// before SQLite can create or open it. It never follows a link in the path.
func secureStatePath(path string, createParent bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == filepath.VolumeName(path)+string(filepath.Separator) {
		return errors.New("sqlite: path must be absolute and clean")
	}
	parent := filepath.Dir(path)
	if err := validateUnixExistingAncestors(parent); err != nil {
		return err
	}
	if createParent {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return err
		}
		if err := validateUnixExistingAncestors(parent); err != nil {
			return err
		}
	}
	if !createParent {
		if _, err := os.Lstat(parent); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
	}
	if err := validateUnixDirectory(parent); err != nil {
		return err
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if !unixOwnedByCurrentUser(parentInfo) {
		return errors.New("sqlite: state directory owner is not the current user")
	}
	if createParent {
		if err := os.Chmod(parent, 0o700); err != nil {
			return err
		}
	}
	for _, candidate := range sqliteArtifacts(path) {
		if err := validateUnixArtifact(candidate, false); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
	}
	return nil
}

func validateUnixDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("sqlite: unsafe state parent")
	}
	return nil
}

// validateUnixDirectoryACL verifies the extended ACL attached to the same
// directory descriptor that was opened without following a final symlink.
func validateUnixDirectoryACL(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unixacl.RejectStateAncestorACLFD(fd)
}

// validateUnixExistingAncestors requires each existing ancestor to be a
// directory that an unprivileged local account cannot replace. This keeps the
// pathname stable between validation and SQLite's later path-based open.
func validateUnixExistingAncestors(path string) error {
	for {
		info, err := os.Lstat(path)
		if err == nil {
			if !isDarwinSystemTempAlias(path) {
				if err := validateUnixDirectory(path); err != nil {
					return err
				}
				if err := validateUnixDirectoryACL(path); err != nil {
					return errors.New("sqlite: state ancestor extended ACL is not private")
				}
				if !unixSafeAncestor(info) {
					return errors.New("sqlite: unsafe writable state ancestor")
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

// unixSafeAncestor accepts private/current-user and root-owned directories
// that no other account can modify. The only writable exception is a
// root-owned sticky directory (for example /tmp): the sticky bit prevents an
// unprivileged account from replacing the current user's existing child.
func unixSafeAncestor(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != uint32(os.Geteuid()) && stat.Uid != 0) {
		return false
	}
	if info.Mode().Perm()&0o022 == 0 {
		return true
	}
	return stat.Uid == 0 && info.Mode()&os.ModeSticky != 0
}

// Darwin exposes the system temporary hierarchy through root-owned /var and
// /tmp aliases. They are the operating system's fixed mount aliases, not
// application-controlled state links; accepting them keeps normal temp paths
// usable while every other existing ancestor remains link-free.
func isDarwinSystemTempAlias(path string) bool {
	if runtime.GOOS != "darwin" || (path != "/var" && path != "/tmp") {
		return false
	}
	target, err := os.Readlink(path)
	return err == nil && (target == "private/var" || target == "private/tmp")
}

func secureStateArtifacts(path string) error {
	parent := filepath.Dir(path)
	if err := validateUnixExistingAncestors(parent); err != nil {
		return err
	}
	if err := validateUnixDirectory(parent); err != nil {
		return err
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if !unixOwnedByCurrentUser(info) {
		return errors.New("sqlite: state directory owner is not the current user")
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return err
	}
	for _, candidate := range sqliteArtifacts(path) {
		if err := validateUnixArtifact(candidate, true); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
	}
	return nil
}

// validateUnixArtifact validates an artifact through a descriptor opened
// without following its final path component. This is required before SQLite
// accesses an existing artifact because a restrictive mode alone does not
// constrain Darwin extended ACL grants.
//
// repairPermissions is used only after SQLite has created or touched the
// artifact. Its second fstat keeps the permission check descriptor-bound after
// the repair instead of re-resolving the pathname.
func validateUnixArtifact(path string, repairPermissions bool) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("sqlite: unsafe state artifact")
	}
	if !unixOwnerMatches(stat.Uid, uint32(os.Geteuid())) {
		return errors.New("sqlite: state artifact owner is not the current user")
	}
	if repairPermissions {
		if err := unix.Fchmod(fd, 0o600); err != nil {
			return err
		}
		if err := unix.Fstat(fd, &stat); err != nil {
			return err
		}
	}
	if stat.Mode&0o077 != 0 {
		return errors.New("sqlite: unsafe state path")
	}
	if err := unixacl.RejectPayloadACLFD(fd); err != nil {
		return errors.New("sqlite: state artifact extended ACL is not private")
	}
	return nil
}

func unixOwnedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && unixOwnerMatches(stat.Uid, uint32(os.Geteuid()))
}

func unixOwnerMatches(uid, currentUID uint32) bool {
	return uid == currentUID
}
