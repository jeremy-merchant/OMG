//go:build windows

package view

import (
	"os"

	"golang.org/x/sys/windows"
)

func platformTTYWidth(file *os.File) (int, bool) {
	if file == nil {
		return 0, false
	}
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(file.Fd()), &info); err != nil {
		return 0, false
	}
	width := int(info.Window.Right-info.Window.Left) + 1
	if width <= 0 {
		return 0, false
	}
	return width, true
}
