//go:build (darwin && macfuse && cgo) || (linux && fuse3 && cgo) || (windows && winfsp && !cgo)

package drive

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/devchux/freesync/internal/storage"
	"github.com/winfsp/cgofuse/fuse"
)

type openFile struct {
	info  storage.FileInfo
	local *os.File
	edit  *workingEdit
}

type workingEdit struct {
	dirty    bool
	snapshot string
	append   bool
}

func (f *filesystem) Create(name string, flags int, _ uint32) (int, uint64) {
	return f.openWritable(name, flags, true)
}

func (f *filesystem) openWritable(name string, flags int, create bool) (int, uint64) {
	if f.queue == nil {
		return -fuse.EROFS, 0
	}
	f.mutations.Lock()
	defer f.mutations.Unlock()
	return f.beginEdit(name, flags, create)
}

func (f *filesystem) beginEdit(name string, flags int, create bool) (int, uint64) {
	key := strings.TrimPrefix(name, "/")
	if key == "" || !visibleName(path.Base(name)) {
		return -fuse.EINVAL, 0
	}
	if f.activeEdit(key) != nil {
		return -fuse.EBUSY, 0
	}
	previous, job, err := f.queue.beginEdit(key)
	if err != nil {
		return fileStatus(err), 0
	}
	opened := false
	defer func() {
		if !opened {
			f.queue.endEdit(key)
		}
	}()
	if previous != nil {
		defer previous.Close()
		if create && flags&fuse.O_EXCL != 0 {
			return -fuse.EEXIST, 0
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	info := storage.FileInfo{Object: storage.Object{Key: key}}
	var missing bool
	if previous != nil {
		info = storage.FileInfo{Object: job.object(), Revision: job.Revision}
	} else {
		info, missing, err = f.editInfo(ctx, name, key, create, flags)
		if err != nil {
			return fileStatus(err), 0
		}
	}
	file, err := os.CreateTemp(f.queue.dir, "edit-*")
	if err != nil {
		return fileStatus(err), 0
	}
	truncate := flags&fuse.O_TRUNC != 0
	if !missing && !truncate {
		var err error
		if previous != nil {
			_, err = io.Copy(file, previous)
		} else {
			err = f.download(ctx, info, file)
		}
		if err != nil {
			file.Close()
			os.Remove(file.Name())
			return fileStatus(err), 0
		}
	}
	opened = true
	return 0, f.addHandle(&openFile{info: info, local: file,
		edit: &workingEdit{dirty: missing || truncate, snapshot: job.ID, append: flags&fuse.O_APPEND != 0}})
}

func (f *filesystem) editInfo(ctx context.Context, name, key string, create bool, flags int) (storage.FileInfo, bool, error) {
	entry, err := f.lookupFresh(ctx, name)
	if create && errors.Is(err, fs.ErrNotExist) {
		parent, err := f.lookupFresh(ctx, path.Dir(name))
		if err != nil {
			return storage.FileInfo{}, false, err
		}
		if !parent.Directory {
			return storage.FileInfo{}, false, fuse.Error(-fuse.ENOTDIR)
		}
		return storage.FileInfo{Object: storage.Object{Key: key}}, true, nil
	}
	if err != nil {
		return storage.FileInfo{}, false, err
	}
	if entry.Directory {
		return storage.FileInfo{}, false, fuse.Error(-fuse.EISDIR)
	}
	if create && flags&fuse.O_EXCL != 0 {
		return storage.FileInfo{}, false, fs.ErrExist
	}
	info, err := f.editableFile(ctx, key)
	return info, false, err
}

func (f *filesystem) editableFile(ctx context.Context, key string) (storage.FileInfo, error) {
	info, err := f.store.Stat(ctx, key)
	if err == nil && (info.Revision == "" || info.Revision == "*") {
		err = errors.New("remote file has no usable revision")
	}
	return info, err
}

func (f *filesystem) download(ctx context.Context, info storage.FileInfo, body *os.File) error {
	if info.Size == 0 {
		return nil
	}
	buffer := make([]byte, min(info.Size, 4<<20))
	for offset := int64(0); offset < info.Size; {
		n, err := f.store.ReadRange(ctx, info, offset, buffer)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		if _, err := body.Write(buffer[:n]); err != nil {
			return err
		}
		offset += int64(n)
	}
	return nil
}

func (f *filesystem) handle(id uint64) *openFile {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open[id]
}

func (f *filesystem) Write(_ string, data []byte, offset int64, handle uint64) int {
	if f.queue == nil {
		return -fuse.EROFS
	}
	f.mutations.Lock()
	defer f.mutations.Unlock()
	file := f.handle(handle)
	if file == nil || file.edit == nil {
		return -fuse.EBADF
	}
	if file.edit.append {
		info, err := file.local.Stat()
		if err != nil {
			return fileStatus(err)
		}
		offset = info.Size()
	}
	n, err := file.local.WriteAt(data, offset)
	if n != 0 {
		file.edit.dirty = true
		return n
	}
	return fileStatus(err)
}

func (f *filesystem) Flush(_ string, handle uint64) int {
	f.mutations.Lock()
	defer f.mutations.Unlock()
	return fileStatus(f.flush(f.handle(handle)))
}

func (f *filesystem) Fsync(name string, _ bool, handle uint64) int {
	return f.Flush(name, handle)
}

func (f *filesystem) flush(file *openFile) error {
	if file == nil || file.edit == nil || !file.edit.dirty {
		return nil
	}
	id, err := f.queue.save(file.info.Key, file.info.Revision, file.edit.snapshot, file.local)
	if err != nil {
		return err
	}
	file.edit.snapshot = id
	file.edit.dirty = false
	return nil
}

func (f *filesystem) release(handle uint64) int {
	file := f.handle(handle)
	if file == nil {
		return 0
	}
	err := f.flush(file)
	f.mu.Lock()
	delete(f.open, handle)
	if file.edit != nil {
		delete(f.writers, file.info.Key)
	}
	f.mu.Unlock()
	if file.local != nil {
		err = errors.Join(err, file.local.Close())
	}
	if file.edit != nil {
		f.queue.endEdit(file.info.Key)
		if err == nil {
			err = os.Remove(file.local.Name())
		} else {
			log.Printf("FreeSync could not queue %q. Local edit retained at %s", file.info.Key, file.local.Name())
		}
	}
	return fileStatus(err)
}

func (f *filesystem) Truncate(name string, size int64, handle uint64) int {
	if f.queue == nil {
		return -fuse.EROFS
	}
	if size < 0 {
		return -fuse.EINVAL
	}
	f.mutations.Lock()
	defer f.mutations.Unlock()
	file := f.handle(handle)
	temporary := file == nil
	if temporary {
		flags := fuse.O_WRONLY
		if size == 0 {
			flags |= fuse.O_TRUNC
		}
		result, id := f.beginEdit(name, flags, false)
		if result != 0 {
			return result
		}
		if size == 0 {
			return f.release(id)
		}
		handle, file = id, f.handle(id)
	}
	if file.edit == nil {
		return -fuse.EBADF
	}
	err := file.local.Truncate(size)
	if err == nil {
		file.edit.dirty = true
	}
	if temporary {
		result := f.release(handle)
		if err == nil {
			return result
		}
	}
	return fileStatus(err)
}
