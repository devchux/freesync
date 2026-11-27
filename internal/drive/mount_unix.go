//go:build (darwin && macfuse && cgo) || (linux && fuse3 && cgo)

package drive

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func mountDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	location := filepath.Join(home, "FreeSync")
	if err := os.Mkdir(location, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(location)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("mount location %q must be a directory", location)
	}
	folder, err := os.Open(location)
	if err != nil {
		return "", err
	}
	_, readErr := folder.Readdirnames(1)
	folder.Close()
	if readErr != io.EOF {
		if readErr != nil {
			return "", readErr
		}
		return "", fmt.Errorf("mount location %q must be empty", location)
	}

	return location, nil
}
