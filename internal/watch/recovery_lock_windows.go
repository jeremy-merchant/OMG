//go:build windows

package watch

import (
	"os"

	"golang.org/x/sys/windows"
)

func acquireRecoveryGuard(path string) (func(), bool) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	created := err == nil
	if os.IsExist(err) {
		file, err = os.OpenFile(path, os.O_RDWR, 0)
	}
	if err != nil {
		return nil, false
	}
	if created {
		if err := secureNewPrivateFile(path); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return nil, false
		}
	}
	info, statErr := os.Lstat(path)
	openedInfo, openedStatErr := file.Stat()
	if statErr != nil || openedStatErr != nil || !validatePrivateRegularFile(path, info) || !validatePrivateRegularFile(path, openedInfo) || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, false
	}

	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		_ = file.Close()
		return nil, false
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
		_ = file.Close()
	}, true
}
