package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/devchux/freesync/internal/storage"
)

type DriveEntry struct {
	Name      string
	Directory bool
	File      storage.Object
}

// ReadDir lists the immediate children of a slash-separated remote directory.
func (s *Storage) ReadDir(ctx context.Context, directory string) ([]DriveEntry, error) {
	prefix := directory
	if prefix != "" {
		prefix += "/"
	}
	objects, err := s.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	children := make(map[string]DriveEntry)
	for _, object := range objects {
		key, ok := strings.CutPrefix(object.Key, prefix)
		if !ok {
			continue
		}
		name, _, isDir := strings.Cut(key, "/")
		if name == "" || name == "." || name == ".." || strings.ContainsRune(name, 0) || !utf8.ValidString(name) {
			continue
		}
		if !children[name].Directory {
			children[name] = DriveEntry{Name: name, Directory: isDir, File: object}
		}
	}
	entries := make([]DriveEntry, 0, len(children))
	for _, entry := range children {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func (s *Storage) Stat(ctx context.Context, key string) (storage.FileInfo, error) {
	return s.backend.Stat(ctx, key)
}

// ReadRange limits a filesystem read to the bytes available before EOF.
func (s *Storage) ReadRange(ctx context.Context, file storage.FileInfo, offset int64, buffer []byte) (int, error) {
	if offset < 0 || file.Size < 0 {
		return 0, errors.New("invalid file offset or size")
	}
	if offset >= file.Size || len(buffer) == 0 {
		return 0, nil
	}
	if remaining := file.Size - offset; remaining < int64(len(buffer)) {
		buffer = buffer[:remaining]
	}
	return s.backend.ReadRange(ctx, file, offset, buffer)
}
