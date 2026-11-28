//go:build (darwin && macfuse && cgo) || (linux && fuse3 && cgo) || (windows && winfsp && !cgo)

package drive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/devchux/freesync/internal/storage"
	"github.com/winfsp/cgofuse/fuse"
)

func (f *filesystem) Unlink(name string) int {
	if f.queue == nil {
		return -fuse.EROFS
	}
	f.mutations.Lock()
	defer f.mutations.Unlock()
	key := strings.TrimPrefix(name, "/")
	if f.activeEdit(key) != nil {
		return -fuse.EBUSY
	}
	if _, ok := f.queue.pending(key); ok {
		return fileStatus(f.queue.discardPending(key))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entry, err := f.lookupFresh(ctx, name)
	if err != nil {
		return fileStatus(err)
	}
	if entry.Directory {
		return -fuse.EISDIR
	}
	info, err := f.editableFile(ctx, key)
	if err != nil {
		return fileStatus(err)
	}
	return fileStatus(f.queue.enqueue(writeJob{Key: key, Revision: info.Revision}))
}

func (f *filesystem) Rename(from, to string) int {
	if f.queue == nil {
		return -fuse.EROFS
	}
	if !visibleName(path.Base(to)) || to == "/" {
		return -fuse.EINVAL
	}
	f.mutations.Lock()
	defer f.mutations.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	entry, err := f.lookupFresh(ctx, from)
	if err != nil {
		return fileStatus(err)
	}
	if from == to {
		return 0
	}
	if entry.Directory {
		return f.renameDirectory(ctx, from, to)
	}
	source, destination := strings.TrimPrefix(from, "/"), strings.TrimPrefix(to, "/")
	if f.activeEdit(source) != nil || f.activeEdit(destination) != nil {
		return -fuse.EBUSY
	}
	if _, ok := f.queue.pending(destination); ok {
		return -fuse.EBUSY
	}
	parent, err := f.lookupFresh(ctx, path.Dir(to))
	if err != nil {
		return fileStatus(err)
	}
	if !parent.Directory {
		return -fuse.ENOTDIR
	}
	var revision string
	target, err := f.lookupFresh(ctx, to)
	switch {
	case err == nil:
		if target.Directory {
			return -fuse.EISDIR
		}
		info, err := f.editableFile(ctx, destination)
		if err != nil {
			return fileStatus(err)
		}
		revision = info.Revision
	case !errors.Is(err, fs.ErrNotExist):
		return fileStatus(err)
	}
	if _, ok := f.queue.pending(source); ok {
		return fileStatus(f.queue.movePending(source, destination, revision))
	}
	info, err := f.editableFile(ctx, source)
	if err != nil {
		return fileStatus(err)
	}
	return fileStatus(f.stageRename(ctx, info, destination, revision))
}

func (f *filesystem) stageRename(ctx context.Context, source storage.FileInfo, destination, revision string) error {
	body, err := os.CreateTemp(f.queue.dir, "upload-*")
	if err != nil {
		return err
	}
	if err := f.queue.seal(body, f.download(ctx, source, body)); err != nil {
		return err
	}
	job := writeJob{Key: destination, Revision: revision, Source: source.Key, SourceRevision: source.Revision,
		Body: filepath.Base(body.Name()), Size: source.Size, Modified: source.LastModified}
	if err := f.queue.enqueue(job); err != nil {
		if errors.Is(err, errJournalUnavailable) {
			return fmt.Errorf("local rename data retained at %s: %w", body.Name(), err)
		}
		f.queue.removeBody(job)
		return err
	}
	return nil
}
