package managed

import (
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/storage"
)

// A trailing slash identifies a directory marker in managed_files. Each marker
// has a unique revision, has no content object, and is hidden from file APIs.
func (s *Server) mutateDirectory(r *http.Request, drive, key string) error {
	ctx := r.Context()
	q := s.queries()
	marker := key + "/"
	children, err := q.ManagedFilesByPrefix(ctx, drive, marker, marker)
	if err != nil {
		return err
	}
	switch r.Method {
	case http.MethodPut:
		if len(children) != 0 {
			return fs.ErrExist
		}
		if err := s.checkDirectoryDestination(r, drive, key); err != nil {
			return err
		}
		return q.InsertManagedFile(ctx, drive, marker, secret(), 0, time.Now().UnixNano())
	case http.MethodDelete:
		if len(children) == 0 {
			return sql.ErrNoRows
		}
		if len(children) != 1 || children[0].Path != marker {
			return storage.ErrDirectoryNotEmpty
		}
		return q.DeleteManagedFile(ctx, drive, marker)
	case http.MethodPost:
		if len(children) == 0 {
			return sql.ErrNoRows
		}
		dest := r.URL.Query().Get("destination")
		if !validPath(dest) || strings.HasPrefix(dest, marker) {
			return fs.ErrInvalid
		}
		if dest == key {
			return nil
		}
		if err := s.checkDirectoryDestination(r, drive, dest); err != nil {
			return err
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		q = database.NewManagedQueries(tx, s.DatabaseType)
		for _, child := range children {
			moved := dest + strings.TrimPrefix(child.Path, key)
			if !validPath(strings.TrimSuffix(moved, "/")) {
				return fs.ErrInvalid
			}
			if err := q.DeleteManagedFile(ctx, drive, child.Path); err != nil {
				return err
			}
			if err := q.InsertManagedFile(ctx, drive, moved, child.Revision, child.Size, child.Modified); err != nil {
				return err
			}
		}
		return tx.Commit()
	default:
		return fs.ErrInvalid
	}
}

func (s *Server) checkDirectoryDestination(r *http.Request, drive, key string) error {
	ctx := r.Context()
	q := s.queries()
	if _, err := q.ManagedFileByPath(ctx, drive, key); !errors.Is(err, sql.ErrNoRows) {
		if err == nil {
			return fs.ErrExist
		}
		return err
	}
	prefix := key + "/"
	if _, err := q.ManagedPathCollision(ctx, drive, prefix, key); !errors.Is(err, sql.ErrNoRows) {
		if err == nil {
			return fs.ErrExist
		}
		return err
	}
	if parent := path.Dir(key); parent != "." {
		prefix := parent + "/"
		rows, err := q.ManagedFilesByPrefix(ctx, drive, prefix, prefix)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}
