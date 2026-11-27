//go:build macfuse && cgo

package drive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/devchux/freesync/internal/service"
	"github.com/devchux/freesync/internal/storage"
	"github.com/winfsp/cgofuse/fuse"
)

type filesystem struct {
	fuse.FileSystemBase
	store *service.Storage
	mu    sync.Mutex
	next  uint64
	open  map[uint64]storage.FileInfo
}

func Mount(store *service.Storage) (err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	location := path.Join(home, "FreeSync")
	if err := os.Mkdir(location, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	info, err := os.Lstat(location)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("mount location %q must be a directory", location)
	}
	folder, err := os.Open(location)
	if err != nil {
		return err
	}
	_, readErr := folder.Readdirnames(1)
	folder.Close()
	if readErr != io.EOF {
		if readErr != nil {
			return readErr
		}
		return fmt.Errorf("mount location %q must be empty", location)
	}

	host := fuse.NewFileSystemHost(&filesystem{store: store, open: make(map[uint64]storage.FileInfo)})
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered != "cgofuse: cannot find FUSE" {
				panic(recovered)
			}
			err = errors.New("macFUSE runtime is required for mounting")
		}
	}()
	fmt.Fprintf(os.Stderr, "Mounting read-only drive at %s. Eject it in Finder or press Ctrl+C to stop.\n", location)
	if !host.Mount(location, []string{"-o", "ro,volname=FreeSync"}) {
		return fmt.Errorf("could not mount FreeSync at %s", location)
	}
	return nil
}

func fileStatus(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, fs.ErrNotExist):
		return -fuse.ENOENT
	case errors.Is(err, storage.ErrFileChanged):
		return -int(syscall.ESTALE)
	default:
		log.Printf("FreeSync drive: %v", err)
		return -fuse.EIO
	}
}

func (f *filesystem) lookup(ctx context.Context, name string) (service.DriveEntry, error) {
	if name == "/" {
		return service.DriveEntry{Directory: true}, nil
	}
	entries, err := f.store.ReadDir(ctx, strings.TrimPrefix(path.Dir(name), "/"))
	if err != nil {
		return service.DriveEntry{}, err
	}
	for _, entry := range entries {
		if entry.Name == path.Base(name) {
			return entry, nil
		}
	}
	return service.DriveEntry{}, fs.ErrNotExist
}

func (f *filesystem) Getattr(name string, stat *fuse.Stat_t, handle uint64) int {
	f.mu.Lock()
	file, open := f.open[handle]
	f.mu.Unlock()
	if open {
		setAttributes(stat, service.DriveEntry{File: file.Object})
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entry, err := f.lookup(ctx, name)
	if err != nil {
		return fileStatus(err)
	}
	setAttributes(stat, entry)
	return 0
}

func setAttributes(stat *fuse.Stat_t, entry service.DriveEntry) {
	stat.Uid, stat.Gid = uint32(os.Getuid()), uint32(os.Getgid())
	if entry.Directory {
		stat.Mode = fuse.S_IFDIR | 0o555
		stat.Nlink = 2
		return
	}
	stat.Mode = fuse.S_IFREG | 0o444
	stat.Nlink = 1
	stat.Size = entry.File.Size
	stat.Mtim = fuse.NewTimespec(entry.File.LastModified)
}

func (f *filesystem) Readdir(name string, fill func(string, *fuse.Stat_t, int64) bool, _ int64, _ uint64) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entry, err := f.lookup(ctx, name)
	if err != nil {
		return fileStatus(err)
	}
	if !entry.Directory {
		return -fuse.ENOTDIR
	}
	entries, err := f.store.ReadDir(ctx, strings.TrimPrefix(name, "/"))
	if err != nil {
		return fileStatus(err)
	}
	if !fill(".", nil, 0) || !fill("..", nil, 0) {
		return 0
	}
	for _, entry := range entries {
		if !fill(entry.Name, nil, 0) {
			break
		}
	}
	return 0
}

func (f *filesystem) Open(name string, flags int) (int, uint64) {
	if flags&(syscall.O_ACCMODE|syscall.O_TRUNC) != syscall.O_RDONLY {
		return -fuse.EROFS, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entry, err := f.lookup(ctx, name)
	if err != nil {
		return fileStatus(err), 0
	}
	if entry.Directory {
		return -fuse.EISDIR, 0
	}
	file, err := f.store.Stat(ctx, entry.File.Key)
	if err != nil {
		return fileStatus(err), 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.open[f.next] = file
	return 0, f.next
}

func (f *filesystem) Read(_ string, buffer []byte, offset int64, handle uint64) int {
	f.mu.Lock()
	file, ok := f.open[handle]
	f.mu.Unlock()
	if !ok {
		return -fuse.EBADF
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := f.store.ReadRange(ctx, file, offset, buffer)
	if err != nil {
		return fileStatus(err)
	}
	return n
}

func (f *filesystem) Release(_ string, handle uint64) int {
	f.mu.Lock()
	delete(f.open, handle)
	f.mu.Unlock()
	return 0
}
