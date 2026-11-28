package service

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/devchux/freesync/internal/storage"
)

// MakeDirectory creates the conventional zero-byte slash marker.
func (s *Storage) MakeDirectory(ctx context.Context, key string) error {
	marker := key + "/"
	objects, err := s.List(ctx, marker)
	if err != nil {
		return err
	}
	if len(objects) != 0 {
		return fs.ErrExist
	}
	writer, ok := s.backend.(queuedWriter)
	if !ok {
		return fs.ErrPermission
	}
	err = writer.PutIfUnchanged(ctx, marker, "", "", strings.NewReader(""))
	if errors.Is(err, storage.ErrFileChanged) {
		return fs.ErrExist
	}
	return err
}

func (s *Storage) RemoveDirectory(ctx context.Context, key string) error {
	marker := key + "/"
	objects, err := s.List(ctx, marker)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return fs.ErrNotExist
	}
	if len(objects) != 1 || objects[0].Key != marker || objects[0].Size != 0 {
		return storage.ErrDirectoryNotEmpty
	}
	info, err := s.Stat(ctx, marker)
	if err != nil {
		return err
	}
	if info.Size != 0 {
		return storage.ErrDirectoryNotEmpty
	}
	return s.CommitDelete(ctx, marker, info.Revision)
}
