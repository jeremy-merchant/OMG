//go:build !darwin

package platform

import "os"

func defaultUserStateDir() (string, error) {
	return os.UserConfigDir()
}
