//go:build !darwin && !linux && !windows

package drive

import (
	"errors"
	"os"
)

func lockSpool(*os.File) error {
	return errors.New("local write queues require macOS, Linux, or Windows")
}
