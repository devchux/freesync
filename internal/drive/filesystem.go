//go:build (darwin && macfuse && cgo) || (linux && fuse3 && cgo) || (windows && winfsp && !cgo)

package drive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/devchux/freesync/internal/cache"
	"github.com/devchux/freesync/internal/service"
	"github.com/devchux/freesync/internal/storage"
	"github.com/winfsp/cgofuse/fuse"
)

type driveReader interface {
	ReadDir(context.Context, string) ([]service.DriveEntry, error)
	Stat(context.Context, string) (storage.FileInfo, error)
	ReadRange(context.Context, storage.FileInfo, int64, []byte) (int, error)
}

type rangeKey struct {
	file   storage.FileInfo
	offset int64
	size   int
}

type filesystem struct {
	fuse.FileSystemBase
	store       driveReader
	directories *cache.Cache[string, []service.DriveEntry]
	metadata    *cache.Cache[string, storage.FileInfo]
	data        *cache.Cache[rangeKey, []byte]
	mu          sync.Mutex
	next        uint64
	open        map[uint64]*openFile
	queue       *writeQueue
	mutations   sync.Mutex
	writers     map[string]*openFile
	stopWrites  func()
}

func mount(store MountStorage, location, options, runtimeName string, writes *WriteConfig, cacheConfig cache.Config) (err error) {
	files := &filesystem{store: store, open: make(map[uint64]*openFile), writers: make(map[string]*openFile)}
	files.directories = cache.New[string, []service.DriveEntry](cacheConfig.MetadataTTL, 64)
	files.metadata = cache.New[string, storage.FileInfo](cacheConfig.MetadataTTL, 1024)
	files.data = cache.New[rangeKey, []byte](cacheConfig.DataTTL, 128)
	if runtime.GOOS == "windows" {
		options += ",FileInfoTimeout=0,DirInfoTimeout=0"
	} else {
		options += ",attr_timeout=0,entry_timeout=0,negative_timeout=0"
		if runtime.GOOS != "linux" {
			options += ",direct_io"
		}
	}
	mode := "read-only"
	if writes != nil {
		if !store.WritableMount() {
			return errors.New("this storage does not support writable mounting")
		}
		queue, err := openWriteQueue(writes.SpoolDir, store, writes.Database)
		if err != nil {
			return err
		}
		files.queue = queue
		queue.invalidate = files.refresh
		mode = "writable"
		defer func() {
			if files.stopWrites != nil {
				files.stopWrites()
			}
			queue.close()
		}()
		if runtime.GOOS != "windows" {
			options += ",default_permissions"
		}
	} else {
		options = "ro," + options
	}
	host := fuse.NewFileSystemHost(files)
	if runtime.GOOS == "linux" {
		host.SetDirectIO(true)
	}
	if files.queue != nil {
		host.SetCapOpenTrunc(true)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered != "cgofuse: cannot find FUSE" && recovered != "cgofuse: cannot find winfsp" {
				panic(recovered)
			}
			err = fmt.Errorf("%s runtime is required for mounting", runtimeName)
		}
	}()
	fmt.Fprintf(os.Stderr, "Mounting %s drive at %s. Press Ctrl+C to stop.\n", mode, location)
	if !host.Mount(location, []string{"-o", options}) {
		return fmt.Errorf("could not mount FreeSync at %s", location)
	}
	return nil
}

func (f *filesystem) refresh() {
	f.directories.Clear()
	f.metadata.Clear()
	f.data.Clear()
}

func (f *filesystem) readMountCommands() {
	input := bufio.NewScanner(os.Stdin)
	for input.Scan() {
		if strings.TrimSpace(input.Text()) == "refresh" {
			f.refresh()
			fmt.Fprintln(os.Stderr, "FreeSync cache cleared. Reopen the directory or refresh it in your file manager.")
		}
	}
}

func (f *filesystem) readDirectory(ctx context.Context, dir string) ([]service.DriveEntry, error) {
	return f.directories.Load(dir, func() ([]service.DriveEntry, error) {
		return f.store.ReadDir(ctx, dir)
	})
}

func (f *filesystem) Init() {
	if f.directories != nil {
		fmt.Fprintln(os.Stderr, "Type refresh and press Enter to clear the mount cache.")
		go f.readMountCommands()
	}
	if f.queue == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	f.stopWrites = func() {
		cancel()
		<-done
	}
	go func() {
		defer close(done)
		f.queue.run(ctx)
	}()
}

func fileStatus(err error) int {
	var mountErr fuse.Error
	switch {
	case errors.As(err, &mountErr):
		return int(mountErr)
	case errors.Is(err, fs.ErrExist):
		return -fuse.EEXIST
	case err == nil:
		return 0
	case errors.Is(err, errWritePending):
		return -fuse.EBUSY
	case errors.Is(err, fs.ErrPermission):
		return -fuse.EACCES
	case errors.Is(err, fs.ErrNotExist):
		return -fuse.ENOENT
	case runtime.GOOS != "windows" && errors.Is(err, storage.ErrFileChanged):
		return -int(syscall.ESTALE)
	default:
		log.Printf("FreeSync drive: %v", err)
		return -fuse.EIO
	}
}

func visibleName(name string) bool {
	if runtime.GOOS != "windows" {
		return true
	}
	return filepath.IsLocal(name) && !strings.ContainsAny(name, `<>:"\|?*`) &&
		!strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ") &&
		strings.IndexFunc(name, func(r rune) bool { return r < 32 }) < 0
}

func (j writeJob) object() storage.Object {
	return storage.Object{Key: j.Key, Size: j.Size, LastModified: j.Modified}
}

func (file *openFile) entry() service.DriveEntry {
	object := file.info.Object
	if file.local != nil {
		if info, err := file.local.Stat(); err == nil {
			object.Size, object.LastModified = info.Size(), info.ModTime()
		}
	}
	return service.DriveEntry{Name: path.Base(object.Key), File: object}
}

func (job writeJob) entry() service.DriveEntry {
	return service.DriveEntry{Name: path.Base(job.Key), File: job.object()}
}

func (f *filesystem) localEntry(key string) (service.DriveEntry, bool) {
	if file := f.activeEdit(key); file != nil {
		return file.entry(), true
	}
	job, ok := f.queue.pending(key)
	if ok && job.Key == key && job.Body != "" {
		return job.entry(), true
	}
	// A queued delete or rename source hides the remote entry.
	return service.DriveEntry{}, ok
}

func (f *filesystem) lookup(ctx context.Context, name string) (service.DriveEntry, error) {
	return f.lookupEntry(ctx, name, f.readDirectory)
}

func (f *filesystem) lookupFresh(ctx context.Context, name string) (service.DriveEntry, error) {
	return f.lookupEntry(ctx, name, f.store.ReadDir)
}

func (f *filesystem) lookupEntry(ctx context.Context, name string, list func(context.Context, string) ([]service.DriveEntry, error)) (service.DriveEntry, error) {
	if name == "/" {
		return service.DriveEntry{Directory: true}, nil
	}
	key := strings.TrimPrefix(name, "/")
	if visibleName(path.Base(name)) && f.queue != nil {
		if entry, handled := f.localEntry(key); handled {
			if entry.Name == "" {
				return service.DriveEntry{}, fs.ErrNotExist
			}
			return entry, nil
		}
	}
	entries, err := list(ctx, strings.TrimPrefix(path.Dir(name), "/"))
	if err != nil {
		return service.DriveEntry{}, err
	}
	for _, entry := range entries {
		if entry.Name == path.Base(name) && visibleName(entry.Name) {
			return entry, nil
		}
	}
	return service.DriveEntry{}, fs.ErrNotExist
}

func (f *filesystem) Getattr(name string, stat *fuse.Stat_t, handle uint64) int {
	if file := f.handle(handle); file != nil {
		f.setAttributes(stat, file.entry())
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entry, err := f.lookup(ctx, name)
	if err != nil {
		return fileStatus(err)
	}
	f.setAttributes(stat, entry)
	return 0
}

func (f *filesystem) setAttributes(stat *fuse.Stat_t, entry service.DriveEntry) {
	if runtime.GOOS != "windows" {
		stat.Uid, stat.Gid = uint32(os.Getuid()), uint32(os.Getgid())
	}
	if entry.Directory {
		stat.Mode = fuse.S_IFDIR | 0o555
		stat.Nlink = 2
		if f.queue != nil {
			stat.Mode = fuse.S_IFDIR | 0o700
		}
		return
	}
	stat.Mode = fuse.S_IFREG | 0o444
	if f.queue != nil {
		stat.Mode = fuse.S_IFREG | 0o600
	}
	stat.Nlink = 1
	stat.Size = entry.File.Size
	stat.Mtim = fuse.NewTimespec(entry.File.LastModified)
}

func (f *filesystem) entries(ctx context.Context, dir string) ([]service.DriveEntry, error) {
	entries, err := f.readDirectory(ctx, dir)
	if err != nil || f.queue == nil {
		return entries, err
	}
	merged := make(map[string]service.DriveEntry, len(entries))
	for _, entry := range entries {
		merged[entry.Name] = entry
	}
	for _, job := range f.queue.changes(dir) {
		if job.Source != "" && parentKey(job.Source) == dir {
			delete(merged, path.Base(job.Source))
		}
		if parentKey(job.Key) != dir {
			continue
		}
		if job.Body != "" {
			entry := job.entry()
			merged[entry.Name] = entry
		} else {
			delete(merged, path.Base(job.Key))
		}
	}
	f.mu.Lock()
	for key, file := range f.writers {
		if parentKey(key) != dir {
			continue
		}
		entry := file.entry()
		merged[entry.Name] = entry
	}
	f.mu.Unlock()
	entries = make([]service.DriveEntry, 0, len(merged))
	for _, entry := range merged {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
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
	entries, err := f.entries(ctx, strings.TrimPrefix(name, "/"))
	if err != nil {
		return fileStatus(err)
	}
	if !fill(".", nil, 0) || !fill("..", nil, 0) {
		return 0
	}
	for _, entry := range entries {
		if visibleName(entry.Name) && !fill(entry.Name, nil, 0) {
			break
		}
	}
	return 0
}

func (f *filesystem) Open(name string, flags int) (int, uint64) {
	if flags&(fuse.O_ACCMODE|fuse.O_TRUNC) != fuse.O_RDONLY {
		return f.openWritable(name, flags, false)
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
	if f.queue != nil {
		if edit := f.activeEdit(entry.File.Key); edit != nil {
			if local, err := os.Open(edit.local.Name()); err == nil {
				return 0, f.addHandle(&openFile{info: edit.info, local: local})
			}
		}
		local, job, err := f.queue.localFile(entry.File.Key)
		if err == nil {
			return 0, f.addHandle(&openFile{info: storage.FileInfo{Object: job.object()}, local: local})
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fileStatus(err), 0
		}
	}
	file, err := f.metadata.Load(entry.File.Key, func() (storage.FileInfo, error) {
		return f.store.Stat(ctx, entry.File.Key)
	})
	if err != nil {
		return fileStatus(err), 0
	}
	return 0, f.addHandle(&openFile{info: file})
}

func (f *filesystem) Read(_ string, buffer []byte, offset int64, handle uint64) int {
	file := f.handle(handle)
	if file == nil {
		return -fuse.EBADF
	}
	if file.local != nil {
		n, err := file.local.ReadAt(buffer, offset)
		if err != nil && err != io.EOF {
			return fileStatus(err)
		}
		return n
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if f.data.Enabled() && len(buffer) <= 256<<10 {
		key := rangeKey{file: file.info, offset: offset, size: len(buffer)}
		data, err := f.data.Load(key, func() ([]byte, error) {
			n, err := f.store.ReadRange(ctx, file.info, offset, buffer)
			return slices.Clone(buffer[:n]), err
		})
		if err != nil {
			return fileStatus(err)
		}
		return copy(buffer, data)
	}
	n, err := f.store.ReadRange(ctx, file.info, offset, buffer)
	if err != nil {
		return fileStatus(err)
	}
	return n
}

func (f *filesystem) addHandle(file *openFile) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.open[f.next] = file
	if file.edit != nil {
		f.writers[file.info.Key] = file
	}
	return f.next
}

func (f *filesystem) activeEdit(key string) *openFile {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writers[key]
}

func (f *filesystem) Release(_ string, handle uint64) int {
	f.mutations.Lock()
	defer f.mutations.Unlock()
	return f.release(handle)
}
