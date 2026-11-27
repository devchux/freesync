package service

import (
	"context"
	"errors"
	"io"
	"io/fs"

	"github.com/devchux/freesync/internal/storage"
)

type queuedWriter interface {
	conditionalWriter
	DeleteIfUnchanged(context.Context, string, string) error
}

func (s *Storage) WritableMount() bool {
	_, ok := s.backend.(queuedWriter)
	return ok
}

// CommitWrite applies a staged upload using the revision observed when it was staged.
// A blank revision means the destination must not exist.
func (s *Storage) CommitWrite(ctx context.Context, key, revision, commitID string, replay bool, body io.ReadSeeker) error {
	writer, ok := s.backend.(queuedWriter)
	if !ok || commitID == "" || revision == "*" {
		return errors.New("conditional upload is unavailable")
	}
	if replay && s.hasCommit(ctx, key, commitID) {
		return nil
	}
	err := writer.PutIfUnchanged(ctx, key, revision, commitID, body)
	if errors.Is(err, storage.ErrFileChanged) && s.hasCommit(ctx, key, commitID) {
		return nil
	}
	return err
}

func (s *Storage) hasCommit(ctx context.Context, key, commitID string) bool {
	file, err := s.Stat(ctx, key)
	return err == nil && file.CommitID == commitID
}

func (s *Storage) CommitDelete(ctx context.Context, key, revision string) error {
	writer, ok := s.backend.(queuedWriter)
	if !ok || revision == "" || revision == "*" {
		return errors.New("conditional deletion requires an existing object revision")
	}
	err := writer.DeleteIfUnchanged(ctx, key, revision)
	if errors.Is(err, storage.ErrFileChanged) {
		_, statErr := s.Stat(ctx, key)
		if errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
	}
	return err
}
