package drive

import (
	"context"

	"github.com/devchux/freesync/internal/service"
	"github.com/devchux/freesync/internal/storage"
)

// MountStorage supplies the operations used by direct and managed mounts.
type MountStorage interface {
	ReadDir(context.Context, string) ([]service.DriveEntry, error)
	Stat(context.Context, string) (storage.FileInfo, error)
	ReadRange(context.Context, storage.FileInfo, int64, []byte) (int, error)
	WritableMount() bool
	writeRemote
}
