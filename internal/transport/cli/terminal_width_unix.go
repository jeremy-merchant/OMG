//go:build !windows

package cli

import (
	"os"

	"golang.org/x/sys/unix"
)

func platformTerminalWidth(file *os.File) (int, bool) {
	if file == nil {
		return 0, false
	}
	window, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
	if err != nil || window.Col == 0 {
		return 0, false
	}
	return int(window.Col), true
}
func platformTerminalHeight(file *os.File) (int, bool) {
	if file == nil {
		return 0, false
	}
	window, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
	if err != nil || window.Row == 0 {
		return 0, false
	}
	return int(window.Row), true
}
