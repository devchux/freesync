package managed

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/storage"
)

type Contents interface {
	storage.Reader
	New(context.Context, string, io.ReadSeeker) error
	Delete(context.Context, string) error
}

type Server struct {
	DB           *sql.DB
	DatabaseType string
	Contents     Contents
	AdminToken   string
	mu           sync.RWMutex
}

func secret() string { return rand.Text() + rand.Text() }
func digest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func validPath(key string) bool {
	return len(key) <= 2048 && key != "." && fs.ValidPath(key) && utf8.ValidString(key) && !strings.ContainsAny(key, "\\:\x00")
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/drives/{drive}/{action}", s.serve)
	return mux
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ctx := r.Context()
	drive, action := r.PathValue("drive"), r.PathValue("action")
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !validPath(drive) || strings.Contains(drive, "/") || len(drive) > 255 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodGet {
		s.mu.RLock()
		defer s.mu.RUnlock()
	} else {
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	admin := s.AdminToken != "" && subtle.ConstantTimeCompare([]byte(digest(token)), []byte(digest(s.AdminToken))) == 1
	switch action {
	case "backup", "create", "invite", "revoke":
		if !admin {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}
	var err error
	switch {
	case admin && action == "backup" && r.Method == http.MethodGet:
		err = s.archive(w, ctx, drive, true)
	case admin && action == "create" && r.Method == http.MethodPost:
		var exists bool
		err = s.DB.QueryRowContext(ctx, s.query("SELECT EXISTS (SELECT 1 FROM managed_drives WHERE id = ?)"), drive).Scan(&exists)
		if err == nil && !exists {
			_, err = s.DB.ExecContext(ctx, s.query("INSERT INTO managed_drives VALUES (?)"), drive)
		}
	case admin && action == "invite" && r.Method == http.MethodPost:
		name := r.URL.Query().Get("member")
		if name == "" || len(name) > 255 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		invitation := secret()
		_, err = s.DB.ExecContext(ctx, s.query("INSERT INTO managed_members VALUES (?, ?, ?, 1, ?)"), drive, name, digest(invitation), permission(r.URL.Query().Get("write") == "true"))
		if err == nil {
			json.NewEncoder(w).Encode(invitation)
		}
	case admin && action == "revoke" && r.Method == http.MethodPost:
		_, err = s.DB.ExecContext(ctx, s.query("DELETE FROM managed_members WHERE drive = ? AND name = ?"), drive, r.URL.Query().Get("member"))
	case action == "accept" && r.Method == http.MethodPost:
		credential := secret()
		var result sql.Result
		result, err = s.DB.ExecContext(ctx, s.query("UPDATE managed_members SET credential = ?, invited = 0 WHERE drive = ? AND credential = ? AND invited = 1"), digest(credential), drive, digest(token))
		if err == nil {
			count, _ := result.RowsAffected()
			if count == 0 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(credential)
		}
	default:
		var member string
		var writable bool
		if !admin {
			err = s.DB.QueryRowContext(ctx, s.query("SELECT credential, writable FROM managed_members WHERE drive = ? AND credential = ? AND invited = 0"), drive, digest(token)).Scan(&member, &writable)
			if errors.Is(err, sql.ErrNoRows) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if err != nil {
				break
			}
		} else {
			writable = true
		}
		if r.Method != http.MethodGet && !writable {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		err = s.files(w, r, drive, member, writable)
	}
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrInvalid):
			w.WriteHeader(http.StatusBadRequest)
		case errors.Is(err, sql.ErrNoRows):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, storage.ErrFileChanged):
			w.WriteHeader(http.StatusConflict)
		default:
			log.Printf("Managed drive %q %s: %v", drive, action, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func (s *Server) stat(ctx context.Context, drive, key string) (storage.FileInfo, error) {
	file := storage.FileInfo{Object: storage.Object{Key: key}}
	var modified int64
	err := s.DB.QueryRowContext(ctx, s.query("SELECT revision, size, modified FROM managed_files WHERE drive = ? AND path = ?"), drive, key).Scan(&file.Revision, &file.Size, &modified)
	file.LastModified = time.Unix(0, modified).UTC()
	return file, err
}

func (s *Server) list(ctx context.Context, drive, prefix string) ([]storage.Object, error) {
	rows, err := s.DB.QueryContext(ctx, s.query("SELECT path, size, modified FROM managed_files WHERE drive = ? AND substr(path, 1, length(?)) = ? ORDER BY path"), drive, prefix, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var objects []storage.Object
	for rows.Next() {
		var object storage.Object
		var modified int64
		if err := rows.Scan(&object.Key, &object.Size, &modified); err != nil {
			return nil, err
		}
		object.LastModified = time.Unix(0, modified).UTC()
		objects = append(objects, object)
	}
	return objects, rows.Err()
}

const objectPrefix = ".freesync-managed/"

func (s *Server) files(w http.ResponseWriter, r *http.Request, drive, member string, writable bool) error {
	ctx := r.Context()
	key, action := r.URL.Query().Get("path"), r.PathValue("action")
	switch {
	case r.Method == http.MethodGet && action == "access":
		var id string
		if err := s.DB.QueryRowContext(ctx, s.query("SELECT id FROM managed_drives WHERE id = ?"), drive).Scan(&id); err != nil {
			return err
		}
		return json.NewEncoder(w).Encode(writable)
	case r.Method == http.MethodGet && action == "export":
		return s.archive(w, ctx, drive, false)
	case r.Method == http.MethodGet && action == "list":
		objects, err := s.list(ctx, drive, r.URL.Query().Get("prefix"))
		if err != nil {
			return err
		}
		return json.NewEncoder(w).Encode(objects)
	}
	if !validPath(key) {
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}
	switch {
	case r.Method == http.MethodGet && action == "stat":
		file, err := s.stat(ctx, drive, key)
		if err != nil {
			return err
		}
		return json.NewEncoder(w).Encode(file)
	case r.Method == http.MethodGet && action == "read":
		file, err := s.stat(ctx, drive, key)
		if err != nil {
			return err
		}
		if file.Revision != r.URL.Query().Get("revision") {
			return storage.ErrFileChanged
		}
		offset, offsetErr := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		size, sizeErr := strconv.Atoi(r.URL.Query().Get("size"))
		if offsetErr != nil || sizeErr != nil || offset < 0 || size < 1 || size > 16<<20 || offset > file.Size || int64(size) > file.Size-offset {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return nil
		}
		buffer := make([]byte, size)
		if err := s.read(ctx, file.Revision, offset, buffer); err != nil {
			return err
		}
		_, err = w.Write(buffer)
		return err
	case (r.Method == http.MethodPut || r.Method == http.MethodDelete) && action == "file":
		return s.commit(r, drive, member, key)
	default:
		w.WriteHeader(http.StatusNotFound)
		return nil
	}
}

func (s *Server) read(ctx context.Context, revision string, offset int64, buffer []byte) error {
	file, err := s.Contents.Stat(ctx, objectPrefix+revision)
	if err != nil {
		return err
	}
	n, err := s.Contents.ReadRange(ctx, file, offset, buffer)
	if err == nil && n != len(buffer) {
		return io.ErrUnexpectedEOF
	}
	return err
}

func (s *Server) commit(r *http.Request, drive, member, key string) error {
	ctx := r.Context()
	base, id := r.URL.Query().Get("revision"), r.URL.Query().Get("commit")
	if len(id) > 128 {
		return fs.ErrInvalid
	}
	deleting := r.Method == http.MethodDelete
	if deleting && base == "" {
		return storage.ErrFileChanged
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldPath, oldBase string
	if id != "" {
		err := tx.QueryRowContext(ctx, s.query("SELECT path, base FROM managed_commits WHERE drive = ? AND member = ? AND id = ?"), drive, member, id).Scan(&oldPath, &oldBase)
		if err == nil {
			if oldPath != key || oldBase != base {
				return storage.ErrFileChanged
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	var revision string
	err = tx.QueryRowContext(ctx, s.query("SELECT revision FROM managed_files WHERE drive = ? AND path = ?"), drive, key).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if deleting && revision == "" {
		return nil
	}
	if revision != base {
		return storage.ErrFileChanged
	}
	if deleting {
		_, err = tx.ExecContext(ctx, s.query("DELETE FROM managed_files WHERE drive = ? AND path = ?"), drive, key)
	} else {
		var collision bool
		err = tx.QueryRowContext(ctx, s.query("SELECT EXISTS (SELECT 1 FROM managed_files WHERE drive = ? AND (instr(path, ?) = 1 OR instr(?, concat(path, '/')) = 1))"), drive, key+"/", key).Scan(&collision)
		if err != nil {
			return err
		}
		if collision {
			return storage.ErrFileChanged
		}
		body, err := os.CreateTemp("", "freesync-save-*")
		if err != nil {
			return err
		}
		defer os.Remove(body.Name())
		defer body.Close()
		size, err := io.Copy(body, r.Body)
		if err != nil {
			return err
		}
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return err
		}
		next := secret()
		if err := s.Contents.New(ctx, objectPrefix+next, body); err != nil {
			return err
		}
		modified := time.Now().UnixNano()
		if revision == "" {
			_, err = tx.ExecContext(ctx, s.query("INSERT INTO managed_files VALUES (?, ?, ?, ?, ?)"), drive, key, next, size, modified)
		} else {
			_, err = tx.ExecContext(ctx, s.query("UPDATE managed_files SET revision = ?, size = ?, modified = ? WHERE drive = ? AND path = ?"), next, size, modified, drive, key)
		}
		if err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	if id != "" {
		if _, err := tx.ExecContext(ctx, s.query("INSERT INTO managed_commits VALUES (?, ?, ?, ?, ?)"), drive, member, id, key, base); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if revision != "" {
		if err := s.Contents.Delete(ctx, objectPrefix+revision); err != nil {
			log.Printf("Reclaim managed revision %s: %v", revision, err)
		}
	}
	return nil
}

func permission(writable bool) int {
	if writable {
		return 1
	}
	return 0
}

func (s *Server) query(query string) string {
	return bind(s.DatabaseType, query)
}

func bind(databaseType, query string) string {
	if databaseType != database.PostgreSQL {
		return query
	}
	query = strings.ReplaceAll(query, "instr(", "strpos(")
	var result strings.Builder
	for i, part := range strings.Split(query, "?") {
		if i > 0 {
			result.WriteByte('$')
			result.WriteString(strconv.Itoa(i))
		}
		result.WriteString(part)
	}
	return result.String()
}
