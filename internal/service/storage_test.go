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

type conditionalMemoryBackend struct {
	memoryBackend
	observedRevision string
	currentRevision  string
}

func (b *conditionalMemoryBackend) Stat(ctx context.Context, key string) (storage.FileInfo, error) {
	file, err := b.memoryBackend.Stat(ctx, key)
	file.Revision = b.observedRevision
	return file, err
}

func (b *conditionalMemoryBackend) PutIfUnchanged(ctx context.Context, key, revision, _ string, body io.ReadSeeker) error {
	_, exists := b.memoryBackend[key]
	if (revision == "" && exists) || (revision != "" && (!exists || revision != b.currentRevision)) {
		return storage.ErrFileChanged
	}
	return b.memoryBackend.Put(ctx, key, body)
}

func TestStorageConditionalWritePreservesRemoteEdits(t *testing.T) {
	ctx := context.Background()
	backend := &conditionalMemoryBackend{
		memoryBackend:    memoryBackend{"file": "remote edit"},
		observedRevision: "older",
		currentRevision:  "newer",
	}
	store := &Storage{backend: backend}

	if err := store.New(ctx, "file", strings.NewReader("duplicate")); !errors.Is(err, ErrObjectExists) {
		t.Fatalf("create existing file returned %v", err)
	}
	if err := store.Update(ctx, "file", strings.NewReader("local edit")); !errors.Is(err, storage.ErrFileChanged) {
		t.Fatalf("update stale file returned %v", err)
	}
	if content := backend.memoryBackend["file"]; content != "remote edit" {
		t.Fatalf("conflict overwrote remote content with %q", content)
	}

	backend.observedRevision = backend.currentRevision
	if err := store.Update(ctx, "file", strings.NewReader("local edit")); err != nil {
		t.Fatal(err)
	}
	if content := backend.memoryBackend["file"]; content != "local edit" {
		t.Fatalf("matching revision did not update content: %q", content)
	}
}

type replayBackend struct {
	memoryBackend
	revision string
	commitID string
	puts     int
}

func (b *replayBackend) Stat(ctx context.Context, key string) (storage.FileInfo, error) {
	file, err := b.memoryBackend.Stat(ctx, key)
	file.Revision, file.CommitID = b.revision, b.commitID
	return file, err
}

func (b *replayBackend) PutIfUnchanged(ctx context.Context, key, revision, commitID string, body io.ReadSeeker) error {
	b.puts++
	if b.revision != revision {
		return storage.ErrFileChanged
	}
	if err := b.memoryBackend.Put(ctx, key, body); err != nil {
		return err
	}
	b.commitID, b.revision = commitID, "next"
	return nil
}

func (b *replayBackend) DeleteIfUnchanged(ctx context.Context, key, revision string) error {
	if b.revision != revision {
		return storage.ErrFileChanged
	}
	return b.memoryBackend.Delete(ctx, key)
}

func TestReplayRecognizesItsOwnCommittedUpload(t *testing.T) {
	ctx := context.Background()
	backend := &replayBackend{memoryBackend: memoryBackend{"file": "original"}, revision: "initial"}
	store := &Storage{backend: backend}
	if err := store.CommitWrite(ctx, "file", "initial", "job-1", false, strings.NewReader("saved")); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitWrite(ctx, "file", "initial", "job-1", true, strings.NewReader("saved")); err != nil {
		t.Fatal(err)
	}
	if backend.puts != 1 {
		t.Fatalf("replay issued %d uploads", backend.puts)
	}
	if err := store.CommitDelete(ctx, "file", "initial"); !errors.Is(err, storage.ErrFileChanged) {
		t.Fatalf("deleting newer revision returned %v", err)
	}
	if backend.memoryBackend["file"] != "saved" {
		t.Fatal("stale delete removed the newer file")
	}
}
