//go:build (darwin && macfuse && cgo) || (linux && fuse3 && cgo) || (windows && winfsp && !cgo)

package drive

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/devchux/freesync/internal/storage"
	"github.com/winfsp/cgofuse/fuse"
)

type directoryWriter interface {
	MakeDirectory(context.Context, string) error
	RemoveDirectory(context.Context, string) error
}

type directoryRenamer interface {
	RenameDirectory(context.Context, string, string) error
}

func (f *filesystem) Mkdir(name string, _ uint32) int {
	return f.mutateDirectory(name, true)
}

func (f *filesystem) Rmdir(name string) int {
	return f.mutateDirectory(name, false)
}

func (f *filesystem) mutateDirectory(name string, create bool) int {
	if f.queue == nil {
		return -fuse.EROFS
	}
	writer, ok := f.store.(directoryWriter)
	if !ok {
		return -fuse.ENOTSUP
	}
	key := strings.TrimPrefix(name, "/")
	if key == "" || !fs.ValidPath(key) || !visibleName(path.Base(name)) {
		return -fuse.EINVAL
	}
	f.mutations.Lock()
	defer f.mutations.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	entry, err := f.lookupFresh(ctx, name)
	if create {
		if err == nil {
			return -fuse.EEXIST
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fileStatus(err)
		}
		parent, err := f.lookupFresh(ctx, path.Dir(name))
		if err != nil {
			return fileStatus(err)
		}
		if !parent.Directory {
			return -fuse.ENOTDIR
		}
		if f.directoryBusy(key) {
			return -fuse.EBUSY
		}
		return f.directoryResult(writer.MakeDirectory(ctx, key))
	}
	if err != nil {
		return fileStatus(err)
	}
	if !entry.Directory {
		return -fuse.ENOTDIR
	}
	if f.directoryBusy(key) {
		return -fuse.EBUSY
	}
	return f.directoryResult(writer.RemoveDirectory(ctx, key))
}

func (f *filesystem) renameDirectory(ctx context.Context, from, to string) int {
	renamer, ok := f.store.(directoryRenamer)
	if !ok {
		return -fuse.ENOTSUP
	}
	source, dest := strings.TrimPrefix(from, "/"), strings.TrimPrefix(to, "/")
	if !fs.ValidPath(dest) || strings.HasPrefix(dest, source+"/") {
		return -fuse.EINVAL
	}
	if f.directoryBusy(source) || f.directoryBusy(dest) {
		return -fuse.EBUSY
	}
	return f.directoryResult(renamer.RenameDirectory(ctx, source, dest))
}

func (f *filesystem) directoryResult(err error) int {
	if errors.Is(err, storage.ErrDirectoryNotEmpty) {
		return -fuse.ENOTEMPTY
	}
	if err == nil {
		f.refresh()
	}
	return fileStatus(err)
}

func (f *filesystem) directoryBusy(key string) bool {
	f.queue.mu.Lock()
	defer f.queue.mu.Unlock()
	prefix := key + "/"
	for held := range f.queue.held {
		if held == key || strings.HasPrefix(held, prefix) {
			return true
		}
	}
	for _, job := range f.queue.jobs {
		if !job.Blocked && (job.Key == key || strings.HasPrefix(job.Key, prefix) ||
			job.Source == key || strings.HasPrefix(job.Source, prefix)) {
			return true
		}
	}
	return false
}
