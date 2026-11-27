//go:build (darwin && macfuse && cgo) || (linux && fuse3 && cgo) || (windows && winfsp && !cgo)

package drive

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/devchux/freesync/internal/service"
	"github.com/devchux/freesync/internal/storage"
	"github.com/winfsp/cgofuse/fuse"
)

func (r *testRemote) Stat(_ context.Context, key string) (storage.FileInfo, error) {
	file, exists := r.files[key]
	if !exists {
		return storage.FileInfo{}, fs.ErrNotExist
	}
	return storage.FileInfo{Object: storage.Object{Key: key, Size: int64(len(file.body))}, Revision: file.revision}, nil
}

func (r *testRemote) ReadDir(ctx context.Context, dir string) ([]service.DriveEntry, error) {
	var entries []service.DriveEntry
	for key := range r.files {
		if parentKey(key) == dir {
			file, err := r.Stat(ctx, key)
			if err != nil {
				return nil, err
			}
			entries = append(entries, service.DriveEntry{Name: key, File: file.Object})
		}
	}
	return entries, nil
}

func (r *testRemote) ReadRange(_ context.Context, file storage.FileInfo, offset int64, buffer []byte) (int, error) {
	n, err := strings.NewReader(r.files[file.Key].body).ReadAt(buffer, offset)
	if err == io.EOF {
		err = nil
	}
	return n, err
}

func TestFilesystemSaveRenameDelete(t *testing.T) {
	ctx := context.Background()
	remote := &testRemote{files: map[string]testObject{"report": {body: "old data", revision: "original"}}}
	queue, err := openTestQueue(t.TempDir(), remote)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.close()
	mount := &filesystem{store: remote, queue: queue, open: make(map[uint64]*openFile), writers: make(map[string]*openFile)}

	code, handle := mount.Create("/.report.tmp", fuse.O_WRONLY|fuse.O_EXCL, 0o600)
	if code != 0 {
		t.Fatalf("create returned %d", code)
	}
	if got := mount.Write("/.report.tmp", []byte("new"), 0, handle); got != 3 {
		t.Fatalf("write returned %d", got)
	}
	if got := mount.Fsync("/.report.tmp", false, handle); got != 0 {
		t.Fatalf("fsync returned %d", got)
	}
	queue.drain(ctx)
	if remote.commits != 0 {
		t.Fatal("upload began while the editor still had the file open")
	}
	if got := mount.Write("/.report.tmp", []byte(" data"), 3, handle); got != 5 {
		t.Fatalf("write after fsync returned %d", got)
	}
	if got := mount.Release("/.report.tmp", handle); got != 0 {
		t.Fatalf("release returned %d", got)
	}
	queue.drain(ctx)
	if remote.commits != 0 {
		t.Fatal("new file uploaded before the editor had a chance to rename it")
	}
	if got := mount.Rename("/.report.tmp", "/report"); got != 0 {
		t.Fatalf("rename returned %d", got)
	}

	code, handle = mount.Open("/report", fuse.O_RDONLY)
	if code != 0 {
		t.Fatalf("open queued file returned %d", code)
	}
	buffer := make([]byte, 8)
	if got := mount.Read("/report", buffer, 0, handle); got != len(buffer) || string(buffer) != "new data" {
		t.Fatalf("read queued file returned %d, %q", got, buffer)
	}
	mount.Release("/report", handle)
	queue.drain(ctx)
	if got := remote.files["report"].body; got != "new data" {
		t.Fatalf("uploaded report = %q", got)
	}
	if _, ok := remote.files[".report.tmp"]; ok {
		t.Fatal("temporary editor file was uploaded")
	}

	if got := mount.Unlink("/report"); got != 0 {
		t.Fatalf("unlink returned %d", got)
	}
	if _, err := mount.lookup(ctx, "/report"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("queued deletion visible in drive: %v", err)
	}
	queue.drain(ctx)
	if _, ok := remote.files["report"]; ok {
		t.Fatal("remote report was not deleted")
	}
}
