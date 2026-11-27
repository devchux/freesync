//go:build fuse3 && cgo

package drive

import "github.com/devchux/freesync/internal/service"

func Mount(store *service.Storage) error {
	location, err := mountDirectory()
	if err != nil {
		return err
	}
	return mount(store, location, "ro,fsname=FreeSync", "FUSE 3")
}
