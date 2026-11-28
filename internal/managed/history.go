package managed

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/devchux/freesync/internal/database"
)

type historyPolicy struct {
	Enabled     bool  `json:"enabled"`
	MaxAgeDays  int64 `json:"maxAgeDays"`
	MaxVersions int64 `json:"maxVersions"`
}

type historyVersion struct {
	Revision string    `json:"revision"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Retired  time.Time `json:"retired"`
}

func loadHistoryPolicy(ctx context.Context, q database.ManagedQueries, drive string) (historyPolicy, error) {
	row, err := q.ManagedHistorySettingsByDrive(ctx, drive)
	if errors.Is(err, sql.ErrNoRows) {
		return historyPolicy{}, nil
	}
	return historyPolicy{Enabled: err == nil, MaxAgeDays: row.MaxAgeDays, MaxVersions: row.MaxVersions}, err
}

func historyPolicies(settings []database.ManagedHistorySetting) map[string]historyPolicy {
	policies := make(map[string]historyPolicy, len(settings))
	for _, row := range settings {
		policies[row.Drive] = historyPolicy{Enabled: true, MaxAgeDays: row.MaxAgeDays, MaxVersions: row.MaxVersions}
	}
	return policies
}

func (p historyPolicy) valid() bool {
	if !p.Enabled {
		return p.MaxAgeDays == 0 && p.MaxVersions == 0
	}
	return p.MaxAgeDays > 0 && p.MaxAgeDays <= 36500 && p.MaxVersions > 0
}

func (p historyPolicy) retains(row database.ManagedVersion, index int64, now time.Time) bool {
	return p.Enabled && index < p.MaxVersions && time.Unix(0, row.Retired).After(now.UTC().AddDate(0, 0, -int(p.MaxAgeDays)))
}

// Rows are ordered by drive, path, and newest retirement first.
func visitVersions(rows []database.ManagedVersion, visit func(database.ManagedVersion, int64) error) error {
	var index int64
	for i, row := range rows {
		if i == 0 || row.Drive != rows[i-1].Drive || row.Path != rows[i-1].Path {
			index = 0
		}
		if err := visit(row, index); err != nil {
			return err
		}
		index++
	}
	return nil
}

func pruneVersions(ctx context.Context, q database.ManagedQueries, rows []database.ManagedVersion, policies map[string]historyPolicy, now time.Time) error {
	return visitVersions(rows, func(row database.ManagedVersion, index int64) error {
		if policies[row.Drive].retains(row, index, now) {
			return nil
		}
		if err := q.InsertManagedReclaim(ctx, row.Revision); err != nil {
			return err
		}
		return q.DeleteManagedVersion(ctx, row.Revision)
	})
}

func (s *Server) historySettings(w http.ResponseWriter, r *http.Request, drive string) error {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		policy, err := loadHistoryPolicy(ctx, s.queries(), drive)
		if err != nil {
			return err
		}
		rows, err := s.queries().ManagedVersionsByDrive(ctx, drive)
		if err != nil {
			return err
		}
		now := time.Now()
		result := struct {
			historyPolicy
			RetainedBytes    int64 `json:"retainedBytes"`
			RetainedVersions int64 `json:"retainedVersions"`
		}{historyPolicy: policy}
		_ = visitVersions(rows, func(row database.ManagedVersion, index int64) error {
			if policy.retains(row, index, now) {
				result.RetainedBytes += row.Size
				result.RetainedVersions++
			}
			return nil
		})
		return json.NewEncoder(w).Encode(result)
	case http.MethodPut:
		var policy historyPolicy
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&policy) != nil || decoder.Decode(new(any)) != io.EOF || !policy.valid() {
			return fs.ErrInvalid
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		q := database.NewManagedQueries(tx, s.DatabaseType)
		if err := q.DeleteManagedHistorySettings(ctx, drive); err != nil {
			return err
		}
		if policy.Enabled {
			if err := q.InsertManagedHistorySettings(ctx, drive, policy.MaxAgeDays, policy.MaxVersions); err != nil {
				return err
			}
		}
		rows, err := q.ManagedVersionsByDrive(ctx, drive)
		if err != nil {
			return err
		}
		if err := pruneVersions(ctx, q, rows, map[string]historyPolicy{drive: policy}, time.Now()); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		s.reclaim(ctx)
		w.WriteHeader(http.StatusNoContent)
		return nil
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
}

func (s *Server) retainedVersions(ctx context.Context, drive, key string) ([]database.ManagedVersion, error) {
	q := s.queries()
	policy, err := loadHistoryPolicy(ctx, q, drive)
	if err != nil || !policy.Enabled {
		return nil, err
	}
	rows, err := q.ManagedVersionsByPath(ctx, drive, key)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i, row := range rows {
		if !policy.retains(row, int64(i), now) {
			return rows[:i], nil
		}
	}
	return rows, nil
}

func (s *Server) listVersions(w http.ResponseWriter, r *http.Request, drive, key string) error {
	rows, err := s.retainedVersions(r.Context(), drive, key)
	if err != nil {
		return err
	}
	result := make([]historyVersion, 0, len(rows))
	for _, row := range rows {
		result = append(result, historyVersion{Revision: row.Revision, Size: row.Size, Modified: time.Unix(0, row.Modified).UTC(), Retired: time.Unix(0, row.Retired).UTC()})
	}
	return json.NewEncoder(w).Encode(result)
}

func (s *Server) restoreVersion(r *http.Request, drive, member, key string) error {
	ctx := r.Context()
	wanted := r.URL.Query().Get("version")
	if wanted == "" || r.URL.Query().Get("commit") != "" {
		return fs.ErrInvalid
	}
	rows, err := s.retainedVersions(ctx, drive, key)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Revision != wanted {
			continue
		}
		return s.commit(r, drive, member, key, func(dst io.Writer) (int64, error) {
			return row.Size, s.copyRevision(ctx, dst, row.Revision, row.Size)
		})
	}
	return sql.ErrNoRows
}

func (s *Server) copyRevision(ctx context.Context, dst io.Writer, revision string, size int64) error {
	buffer := make([]byte, min(size, 1<<20))
	for offset := int64(0); offset < size; {
		chunk := buffer[:min(int64(len(buffer)), size-offset)]
		if err := s.read(ctx, revision, offset, chunk); err != nil {
			return err
		}
		if _, err := dst.Write(chunk); err != nil {
			return err
		}
		offset += int64(len(chunk))
	}
	return nil
}

// MaintainHistory expires versions and retries storage deletions while the server is running.
func (s *Server) MaintainHistory(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for ctx.Err() == nil {
		s.mu.Lock()
		if err := s.sweepHistory(ctx); err != nil && ctx.Err() == nil {
			log.Printf("Sweep managed history: %v", err)
		}
		s.reclaim(ctx)
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) sweepHistory(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := database.NewManagedQueries(tx, s.DatabaseType)
	settings, err := q.AllManagedHistorySettings(ctx)
	if err != nil {
		return err
	}
	rows, err := q.AllManagedVersions(ctx)
	if err != nil {
		return err
	}
	if err := pruneVersions(ctx, q, rows, historyPolicies(settings), time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

// The queue only contains unattached uploads or revisions whose references were removed
// atomically. Hold the server write lock across both the reference change and reclamation.
func (s *Server) reclaim(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	q := s.queries()
	rows, err := q.AllManagedReclaims(ctx)
	if err != nil {
		log.Printf("List managed reclaims: %v", err)
		return
	}
	for _, revision := range rows {
		if ctx.Err() != nil {
			return
		}
		err := s.Contents.Delete(ctx, objectPrefix+revision)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			err = q.DeleteManagedReclaim(ctx, revision)
		}
		if err != nil {
			log.Printf("Reclaim managed revision %s: %v", revision, err)
		}
	}
}
