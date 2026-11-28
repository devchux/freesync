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
	authMu       sync.Mutex
	authWindow   time.Time
	authAttempts int
}

func secret() string { return rand.Text() + rand.Text() }
func digest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func validPath(key string) bool {
	return len(key) <= 2048 && key != "." && fs.ValidPath(key) && utf8.ValidString(key) && !strings.ContainsAny(key, "\\:\x00")
}

// Protect applies the managed API's origin checks and response headers.
func (s *Server) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !sameOrigin(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HandleDrive handles operations on an individual managed drive.
func (s *Server) HandleDrive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	drive, action := r.PathValue("drive"), r.PathValue("action")
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
	if action == "backup" || action == "claim" {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.AdminToken == "" || subtle.ConstantTimeCompare([]byte(digest(token)), []byte(digest(s.AdminToken))) != 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case action == "backup" && r.Method == http.MethodGet:
			s.respondError(w, s.archive(w, ctx, drive, true))
		case action == "claim" && r.Method == http.MethodPost:
			s.respondError(w, s.claim(ctx, drive, r.URL.Query().Get("email")))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
		return
	}
	account, err := s.account(r)
	if err != nil {
		s.accountError(w, err)
		return
	}
	if action == "create" && r.Method == http.MethodPost {
		s.respondError(w, s.createDrive(ctx, drive, account.ID))
		return
	}

	ownerID, err := s.queries().ManagedDriveOwner(ctx, drive)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if err != nil {
		s.respondError(w, err)
		return
	}
	owner := ownerID == account.ID
	switch action {
	case "invite", "revoke", "members":
		if !owner {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		s.respondError(w, s.manageMembers(w, r, drive, ownerID))
		return
	}
	if action == "accept" && r.Method == http.MethodPost {
		s.respondError(w, s.accept(ctx, drive, account.ID))
		return
	}
	writable := owner
	if !owner {
		grant, err := s.queries().ManagedGrantByAccount(ctx, drive, account.ID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && grant.Pending != 0) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if err != nil {
			s.respondError(w, err)
			return
		}
		writable = grant.Writable != 0
	}
	if r.Method != http.MethodGet && !writable {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	s.respondError(w, s.files(w, r, drive, account.ID, writable))
}

func (s *Server) respondError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, fs.ErrInvalid):
		w.WriteHeader(http.StatusBadRequest)
	case errors.Is(err, fs.ErrPermission):
		w.WriteHeader(http.StatusForbidden)
	case errors.Is(err, sql.ErrNoRows):
		w.WriteHeader(http.StatusNotFound)
	case errors.Is(err, storage.ErrFileChanged), errors.Is(err, fs.ErrExist):
		w.WriteHeader(http.StatusConflict)
	default:
		log.Printf("Managed request: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *Server) stat(ctx context.Context, drive, key string) (storage.FileInfo, error) {
	file, err := s.queries().ManagedFileByPath(ctx, drive, key)
	return storage.FileInfo{Object: managedObject(file), Revision: file.Revision}, err
}

func managedObject(file database.ManagedFile) storage.Object {
	return storage.Object{Key: file.Path, Size: file.Size, LastModified: time.Unix(0, file.Modified).UTC()}
}

func (s *Server) list(ctx context.Context, drive, prefix string) ([]storage.Object, error) {
	files, err := s.queries().ManagedFilesByPrefix(ctx, drive, prefix, prefix)
	if err != nil {
		return nil, err
	}
	var objects []storage.Object
	for _, file := range files {
		objects = append(objects, managedObject(file))
	}
	return objects, nil
}

const objectPrefix = ".freesync-managed/"

func (s *Server) files(w http.ResponseWriter, r *http.Request, drive, member string, writable bool) error {
	ctx := r.Context()
	key, action := r.URL.Query().Get("path"), r.PathValue("action")
	switch {
	case r.Method == http.MethodGet && action == "access":
		if _, err := s.queries().ManagedDriveByID(ctx, drive); err != nil {
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
	queries := database.NewManagedQueries(tx, s.DatabaseType)
	if id != "" {
		receipt, err := queries.ManagedCommitByID(ctx, drive, member, id)
		if err == nil {
			if receipt.Path != key || receipt.Base != base {
				return storage.ErrFileChanged
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	current, err := queries.ManagedFileByPath(ctx, drive, key)
	revision := current.Revision
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
		if err := queries.DeleteManagedFile(ctx, drive, key); err != nil {
			return err
		}
	} else {
		_, err := queries.ManagedPathCollision(ctx, drive, key+"/", key)
		if err == nil {
			return storage.ErrFileChanged
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
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
			err = queries.InsertManagedFile(ctx, drive, key, next, size, modified)
		} else {
			err = queries.UpdateManagedFile(ctx, next, size, modified, drive, key)
		}
		if err != nil {
			return err
		}
	}
	if id != "" {
		if err := queries.InsertManagedCommit(ctx, drive, member, id, key, base); err != nil {
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

func permission(writable bool) int64 {
	if writable {
		return 1
	}
	return 0
}

func (s *Server) queries() database.ManagedQueries {
	return database.NewManagedQueries(s.DB, s.DatabaseType)
}
