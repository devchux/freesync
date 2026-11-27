package service

import (
	"context"
	"errors"
	"io"
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
