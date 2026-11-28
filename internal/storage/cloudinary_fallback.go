package storage

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
)

// Keep one complete revision per mount so the per-file and total limits coincide.
type cloudinaryFallback struct {
	mu    sync.Mutex
	dir   string
	limit int64
	info  FileInfo
	file  *os.File
}

// EnableDownloadFallback must be called before the mount starts reading.
func (c *Cloudinary) EnableDownloadFallback(limit int64) (func(), error) {
	dir, err := os.MkdirTemp("", "freesync-cloudinary-*")
	if err != nil {
		return nil, err
	}
	fallback := &cloudinaryFallback{dir: dir, limit: limit}
	c.fallback = fallback
	return func() {
		fallback.mu.Lock()
		defer fallback.mu.Unlock()
		fallback.evict()
		_ = os.Remove(dir)
	}, nil
}

func (f *cloudinaryFallback) evict() {
	if f.file != nil {
		_ = f.file.Close()
		_ = os.Remove(f.file.Name())
		f.file = nil
	}
}

func (f *cloudinaryFallback) readFull(res *http.Response, info FileInfo, offset int64, buffer []byte) (int, error) {
	defer res.Body.Close()
	if info.Size < 0 || info.Size > f.limit {
		return 0, fmt.Errorf("%w: asset exceeds Cloudinary fallback budget of %d bytes", ErrRangeUnsupported, f.limit)
	}
	if res.ContentLength >= 0 && res.ContentLength != info.Size {
		return 0, fmt.Errorf("Cloudinary asset size changed: %w", ErrFileChanged)
	}
	encoding := res.Header.Get("Content-Encoding")
	if res.Uncompressed || (encoding != "" && encoding != "identity") {
		return 0, fmt.Errorf("unexpected encoding for full Cloudinary response")
	}

	f.evict()
	local, err := os.CreateTemp(f.dir, "asset-*")
	if err != nil {
		return 0, err
	}
	defer func() {
		if f.file != local {
			_ = local.Close()
			_ = os.Remove(local.Name())
		}
	}()
	if _, err := io.CopyN(local, res.Body, info.Size); err != nil {
		return 0, err
	}
	var extra [1]byte
	if _, err := io.ReadFull(res.Body, extra[:]); err != io.EOF {
		if err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("Cloudinary asset size changed: %w", ErrFileChanged)
	}
	f.file = local
	f.info = info
	return local.ReadAt(buffer, offset)
}
