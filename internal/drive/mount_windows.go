//go:build winfsp && !cgo

package drive

import (
	"errors"
	"fmt"

	"github.com/devchux/freesync/internal/service"
	"golang.org/x/sys/windows"
)

func Mount(store *service.Storage, writes *WriteConfig) error {
	drives, err := windows.GetLogicalDrives()
	if err != nil {
		return fmt.Errorf("find an available drive letter: %w", err)
	}
	for letter := 'F'; letter <= 'Z'; letter++ {
		if drives&(1<<(letter-'A')) == 0 {
			return mount(store, string(letter)+":", "volname=FreeSync,uid=-1,gid=-1", "WinFsp", writes)
		}
	}
	return errors.New("no available drive letter from F: through Z:")
}
