//go:build darwin || linux

package drive

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockSpool(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
