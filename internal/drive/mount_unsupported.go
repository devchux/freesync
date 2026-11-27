//go:build !darwin || !macfuse || !cgo

package drive

import (
	"errors"

	"github.com/devchux/freesync/internal/service"
)

func Mount(_ *service.Storage) error {
	return errors.New("mounting requires macOS, macFUSE, and a FreeSync build with CGO and -tags macfuse")
}
