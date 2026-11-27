package service

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/devchux/freesync/internal/storage"
)

type memoryBackend map[string]string

func (b memoryBackend) Put(_ context.Context, key string, body io.ReadSeeker) error {
	content, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	b[key] = string(content)
	return nil
}

func (b memoryBackend) List(_ context.Context, prefix string) ([]storage.Object, error) {
	var objects []storage.Object
	for key, content := range b {
		if strings.HasPrefix(key, prefix) {
			objects = append(objects, storage.Object{Key: key, Size: int64(len(content))})
		}
	}
	return objects, nil
}

func (b memoryBackend) Delete(_ context.Context, key string) error {
	delete(b, key)
	return nil
}

func TestStorageObjectLifecycle(t *testing.T) {
	ctx := context.Background()
	backend := memoryBackend{"file-old": "keep"}
	store := &Storage{backend: backend}

	if err := store.Update(ctx, "file", strings.NewReader("update")); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("update missing key returned %v", err)
	}
	if err := store.New(ctx, "file", strings.NewReader("original")); err != nil {
		t.Fatal(err)
	}
	if err := store.New(ctx, "file", strings.NewReader("duplicate")); !errors.Is(err, ErrObjectExists) {
		t.Fatalf("create duplicate key returned %v", err)
	}
	if got := backend["file"]; got != "original" {
		t.Fatalf("duplicate create changed content to %q", got)
	}
	if err := store.Update(ctx, "file", strings.NewReader("updated")); err != nil {
		t.Fatal(err)
	}
	if got := backend["file"]; got != "updated" {
		t.Fatalf("updated content = %q", got)
	}
	if err := store.Delete(ctx, "file"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, "file"); err != nil {
		t.Fatal(err)
	}
	objects, err := store.List(ctx, "file")
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || objects[0].Key != "file-old" {
		t.Fatalf("list after deletion = %+v", objects)
	}
}

func (b memoryBackend) Stat(_ context.Context, key string) (storage.FileInfo, error) {
	content, ok := b[key]
	if !ok {
		return storage.FileInfo{}, fs.ErrNotExist
	}
	return storage.FileInfo{Object: storage.Object{Key: key, Size: int64(len(content))}}, nil
}

func (b memoryBackend) ReadRange(_ context.Context, file storage.FileInfo, offset int64, buffer []byte) (int, error) {
	return strings.NewReader(b[file.Key]).ReadAt(buffer, offset)
}

func TestStorageDriveReads(t *testing.T) {
	ctx := context.Background()
	store := &Storage{backend: memoryBackend{
		"docs":           "key collides with folder",
		"docs/readme":    "0123456789",
		"docs/sub/image": "image",
		"docs-other":     "outside",
	}}
	root, err := store.ReadDir(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(root) != 2 || root[0].Name != "docs" || !root[0].Directory {
		t.Fatalf("unexpected root entries: %+v", root)
	}
	docs, err := store.ReadDir(ctx, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Name != "readme" || docs[0].Directory || docs[1].Name != "sub" || !docs[1].Directory {
		t.Fatalf("unexpected docs entries: %+v", docs)
	}

	file, err := store.Stat(ctx, "docs/readme")
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 8)
	n, err := store.ReadRange(ctx, file, 7, buffer)
	if err != nil || string(buffer[:n]) != "789" {
		t.Fatalf("range near EOF = %q, %v", buffer[:n], err)
	}
	n, err = store.ReadRange(ctx, file, file.Size, buffer)
	if err != nil || n != 0 {
		t.Fatalf("range at EOF = %d bytes, %v", n, err)
	}
}
