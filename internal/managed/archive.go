package managed

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/service"
	"github.com/devchux/freesync/internal/storage"
)

func (s *Server) archive(w http.ResponseWriter, ctx context.Context, drive string, backup bool) error {
	var snapshot []byte
	var files []storage.FileInfo
	if backup {
		meta, err := readMetadata(ctx, s.queries())
		if err != nil {
			return err
		}
		files, err = meta.contents()
		if err != nil {
			return err
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Revision < files[j].Revision })
		snapshot, err = json.Marshal(meta)
		if err != nil {
			return err
		}
	} else {
		objects, err := s.list(ctx, drive, "")
		if err != nil {
			return err
		}
		for _, object := range objects {
			if strings.HasSuffix(object.Key, "/") {
				files = append(files, storage.FileInfo{Object: object})
				continue
			}
			file, err := s.stat(ctx, drive, object.Key)
			if err != nil {
				return err
			}
			files = append(files, file)
		}
	}
	w.Header().Set("Content-Type", "application/x-tar")
	archive := tar.NewWriter(w)
	if snapshot != nil {
		if err := archive.WriteHeader(&tar.Header{Name: "metadata.json", Size: int64(len(snapshot)), Mode: 0600}); err != nil {
			return err
		}
		if _, err := archive.Write(snapshot); err != nil {
			return err
		}
	}
	for _, file := range files {
		name := file.Key
		if !backup && strings.HasSuffix(name, "/") {
			if err := archive.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0700, ModTime: file.LastModified}); err != nil {
				return err
			}
			continue
		}
		if backup {
			name = "objects/" + file.Revision
		}
		if err := archive.WriteHeader(&tar.Header{Name: name, Size: file.Size, Mode: 0600, ModTime: file.LastModified}); err != nil {
			return err
		}
		if err := s.copyRevision(ctx, archive, file.Revision, file.Size); err != nil {
			// Interrupt the HTTP stream so clients cannot mistake a partial archive for success.
			panic(http.ErrAbortHandler)
		}
	}
	return archive.Close()
}

// Restore commits metadata only after all referenced objects are present.
func Restore(ctx context.Context, config database.Config, contents Contents, source io.Reader) error {
	dir, err := os.MkdirTemp("", "freesync-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	archive := tar.NewReader(source)
	meta, err := archiveMetadata(ctx, archive, dir)
	if err != nil {
		return err
	}
	files, err := meta.contents()
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Revision < files[j].Revision })

	db, err := database.Open(ctx, config)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := database.NewManagedQueries(tx, config.Type)
	for _, find := range []func(context.Context) (string, error){queries.AnyManagedDrive, queries.AnyManagedAccount} {
		_, err := find(ctx)
		if err == nil {
			return errors.New("restore requires a database with no managed drives or accounts")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if err := meta.insert(ctx, queries); err != nil {
		return err
	}

	for _, file := range files {
		header, err := archive.Next()
		if err != nil {
			return fmt.Errorf("missing backup contents for %s: %w", file.Revision, err)
		}
		if header.Typeflag != tar.TypeReg || header.Name != "objects/"+file.Revision || header.Size != file.Size {
			return errors.New("unexpected backup contents entry")
		}
		if err := restoreObject(ctx, dir, contents, file.Revision, archive); err != nil {
			return err
		}
	}
	if _, err := archive.Next(); err != io.EOF {
		return errors.New("unexpected data after backup contents")
	}
	return tx.Commit()
}

func archiveMetadata(ctx context.Context, archive *tar.Reader, dir string) (metadata, error) {
	header, err := archive.Next()
	if err != nil {
		return metadata{}, err
	}
	if header.Typeflag != tar.TypeReg {
		return metadata{}, errors.New("invalid backup metadata entry")
	}
	switch header.Name {
	case "metadata.json":
		var meta metadata
		if err := json.NewDecoder(archive).Decode(&meta); err != nil {
			return metadata{}, err
		}
		if meta.Version < 1 || meta.Version > 3 {
			return metadata{}, fmt.Errorf("unsupported backup version %d", meta.Version)
		}
		return meta, nil
	case "metadata.sqlite":
		filename := filepath.Join(dir, "metadata.sqlite")
		if err := extract(filename, archive); err != nil {
			return metadata{}, err
		}
		db, err := database.Open(ctx, database.Config{Type: database.SQLite, Address: filename})
		if err != nil {
			return metadata{}, err
		}
		defer db.Close()
		return readMetadata(ctx, database.NewManagedQueries(db, database.SQLite))
	default:
		return metadata{}, errors.New("missing backup metadata")
	}
}

func extract(filename string, source io.Reader) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, source)
	return errors.Join(err, file.Close())
}

func restoreObject(ctx context.Context, dir string, contents Contents, revision string, source io.Reader) error {
	filename := filepath.Join(dir, "object")
	if err := extract(filename, source); err != nil {
		return err
	}
	defer os.Remove(filename)
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	key := objectPrefix + revision
	err = contents.New(ctx, key, file)
	if !errors.Is(err, service.ErrObjectExists) && !errors.Is(err, os.ErrExist) {
		return err
	}
	// A previous restore may have uploaded this object before it was interrupted.
	info, err := contents.Stat(ctx, key)
	if err != nil {
		return err
	}
	local, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size != local.Size() {
		return fmt.Errorf("existing revision %s differs from backup", revision)
	}
	expected, actual := make([]byte, 1<<20), make([]byte, 1<<20)
	for offset := int64(0); offset < info.Size; {
		size := min(int64(len(expected)), info.Size-offset)
		if _, err := file.ReadAt(expected[:size], offset); err != nil {
			return err
		}
		n, err := contents.ReadRange(ctx, info, offset, actual[:size])
		if err != nil {
			return err
		}
		if int64(n) != size || !bytes.Equal(expected[:size], actual[:size]) {
			return fmt.Errorf("existing revision %s differs from backup", revision)
		}
		offset += size
	}
	return nil
}
