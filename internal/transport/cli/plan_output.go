package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

type privatePlanOutputFile interface {
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
	Stat() (os.FileInfo, error)
}

var createNewPrivatePlanOutputFile = func(path string) (privatePlanOutputFile, error) {
	return createNewPrivatePlanFile(path)
}

func removePrivatePlanOutputIfSameFile(path string, created privatePlanOutputFile) {
	opened, err := created.Stat()
	if err != nil {
		return
	}
	current, err := os.Lstat(path)
	if err == nil && os.SameFile(opened, current) {
		_ = os.Remove(path)
	}
}

var _ privatePlanOutputFile = (*os.File)(nil)

// writeNewPrivatePlan writes a migration plan only to a new, private regular
// file. Platform implementations open the parent without following links and
// create the leaf exclusively.
func writeNewPrivatePlan(path string, data []byte) error {
	if path == "" {
		return errors.New("empty plan output path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	file, err := createNewPrivatePlanOutputFile(absolute)
	if err != nil {
		return err
	}
	if _, err := file.Stat(); err != nil {
		_ = file.Close()
		return err
	}
	complete := false
	defer func() {
		if !complete {
			// Compare against the still-open handle. An unlinked Unix inode
			// cannot be reused while this handle remains open, and Windows
			// denies replacement because the handle has no delete sharing.
			removePrivatePlanOutputIfSameFile(absolute, file)
		}
		_ = file.Close()
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := writeAllPrivate(file, data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func writeAllPrivate(file io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := file.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
