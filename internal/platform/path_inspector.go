package platform

import (
	"errors"
	"os"
	"path/filepath"
)

// PathInspector performs host-filesystem checks without mutating inspected paths.
type PathInspector struct{}

func NewPathInspector() PathInspector { return PathInspector{} }

// FreshDestination reports whether path is absent beneath an existing,
// symlink-free directory chain on its current volume.
func (PathInspector) FreshDestination(path string) bool {
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	volume := filepath.VolumeName(parent) + string(filepath.Separator)
	for current := parent; current != volume; current = filepath.Dir(current) {
		info, err = os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		next := filepath.Dir(current)
		if next == current {
			break
		}
	}
	return true
}

// SameDirectory compares existing directories by filesystem identity after
// requiring the candidate to use a canonical absolute spelling.
func (PathInspector) SameDirectory(candidate, selected string) bool {
	if !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
		return false
	}
	candidateInfo, err := os.Stat(candidate)
	if err != nil || !candidateInfo.IsDir() {
		return false
	}
	selectedInfo, err := os.Stat(selected)
	return err == nil && selectedInfo.IsDir() && os.SameFile(candidateInfo, selectedInfo)
}
