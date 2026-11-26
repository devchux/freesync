package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/http"
	"github.com/devchux/freesync/internal/route"
	"github.com/devchux/freesync/internal/service"
)

type status struct {
	Folder    string    `json:"folder"`
	LastScan  time.Time `json:"lastScan"`
	FileCount int       `json:"fileCount"`
	ScanError string    `json:"scanError,omitempty"`
}

type catalog struct {
	mu      sync.RWMutex
	status  status
	entries []service.CatalogEntry
}

func NewCatalog(
	status status,
	entries []service.CatalogEntry) *catalog {
	var ctg *catalog
	ctg = &catalog{
		status:  status,
		entries: entries,
	}

	return ctg
}

func (c *catalog) snapshotStatus() status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *catalog) snapshotFiles() []service.CatalogEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries
}

func (c *catalog) scan(ctx context.Context, store *service.Catalog) error {
	root := c.status.Folder
	entries := make([]service.CatalogEntry, 0)
	count := 0
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := item.Info()
		if err != nil {
			if path != root && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if path == root {
			if !info.IsDir() {
				return fmt.Errorf("%q is not a directory", root)
			}
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file := service.CatalogEntry{Path: filepath.ToSlash(relative), Directory: info.IsDir()}
		if !file.Directory {
			file.Size = info.Size()
			count++
		}
		entries = append(entries, file)
		return nil
	})

	if err == nil {
		if saveErr := store.ReplaceEntries(ctx, root, entries); saveErr != nil {
			err = fmt.Errorf("persist catalog: %w", saveErr)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.status.ScanError = "Folder scan failed"
		return err
	}
	c.entries = entries
	c.status.LastScan = time.Now().UTC()
	c.status.FileCount = count
	c.status.ScanError = ""
	return nil
}

// Run scans a local folder and serves its inventory over loopback.
func Run(ctx context.Context, folder string, port int, interval time.Duration, dbPath string) error {
	root, err := filepath.Abs(folder)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	db, err := database.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store := service.NewCatalog(db)

	c := NewCatalog(status{Folder: root}, nil)
	if err := c.scan(ctx, store); err != nil {
		return fmt.Errorf("initial scan: %w", err)
	}

	server, err := http.Listen(port, route.New(c.snapshotStatus, c.snapshotFiles))
	if err != nil {
		return err
	}

	log.Printf("Scanning %s every %s. Local API at http://%s", root, interval, server.Addr())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve() }()

	scanCtx, cancelScan := context.WithCancel(ctx)
	scansDone := make(chan struct{})
	go func() {
		defer close(scansDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-scanCtx.Done():
				return
			case <-ticker.C:
				if err := c.scan(scanCtx, store); err != nil && scanCtx.Err() == nil {
					log.Printf("Folder scan: %v", err)
				}
			}
		}
	}()
	defer func() {
		cancelScan()
		<-scansDone
	}()

	select {
	case err := <-serverDone:
		return err
	case <-ctx.Done():
		return server.Shutdown()
	}
}
