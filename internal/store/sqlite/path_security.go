package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func sqliteArtifacts(path string) []string {
	return []string{path, path + "-wal", path + "-shm", path + "-journal"}
}

func stateFileExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func sqliteFileURI(path string) string {
	// SQLite URI paths use slash separators and must escape URI-significant bytes.
	uri := filepath.ToSlash(path)
	uri = strings.ReplaceAll(uri, "%", "%25")
	uri = strings.ReplaceAll(uri, "?", "%3F")
	uri = strings.ReplaceAll(uri, "#", "%23")
	return "file:" + uri
}

func sqliteURI(path string, readOnly, existingOnly bool) string {
	uri := sqliteFileURI(path)
	if readOnly {
		return uri + "?mode=ro&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=mmap_size(0)"
	}
	if existingOnly {
		return uri + "?mode=rw&_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=mmap_size(0)&_pragma=synchronous(FULL)"
	}
	return uri + "?_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=mmap_size(0)&_pragma=synchronous(FULL)"
}
