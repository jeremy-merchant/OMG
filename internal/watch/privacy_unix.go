//go:build !windows

package watch

import (
	"os"
	"path/filepath"
)

func validateStateDir(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return ErrInvalidConfig
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return ErrInvalidConfig
	}
	return nil
}

func validatePrivateRegularFile(_ string, info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0
}

func secureNewPrivateFile(path string) error {
	return os.Chmod(path, 0o600)
}
