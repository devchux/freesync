//go:build !(darwin && macfuse && cgo) && !(linux && fuse3 && cgo) && !(windows && winfsp && !cgo)

package drive

import (
	"fmt"
	"runtime"

	"github.com/devchux/freesync/internal/cache"
)

func Mount(_ MountStorage, _ *WriteConfig, _ cache.Config) error {
	return fmt.Errorf("mounting is unavailable in this %s build. See the FreeSync README for platform-specific build instructions", runtime.GOOS)
}
