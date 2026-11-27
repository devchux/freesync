package drive

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/service"
)

// WriteConfig selects a metadata database and a directory for local file data.
type WriteConfig struct {
	SpoolDir string
	Database database.Config
}

const journalTimeout = 10 * time.Second

func openWriteQueue(dir string, remote writeRemote, config database.Config) (*writeQueue, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	q := &writeQueue{dir: dir, remote: remote, jobs: make(map[string]writeJob), held: make(map[string]bool)}
	if err := q.openSpool(config); err != nil {
		q.close()
		return nil, err
	}
	return q, nil
}

func (q *writeQueue) openSpool(config database.Config) error {
	var err error
	q.lock, err = os.OpenFile(filepath.Join(q.dir, "mount.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := lockSpool(q.lock); err != nil {
		return fmt.Errorf("lock local write spool: %w", err)
	}
	id, err := q.spoolID()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	q.journal, err = service.OpenWriteJournal(ctx, config, id)
	if err != nil {
		return fmt.Errorf("open write queue database: %w", err)
	}
	records, err := q.journal.List(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		job, err := decodeJob(record)
		if err != nil {
			return err
		}
		q.jobs[job.ID] = job
	}
	return q.importLegacy(ctx)
}

func (q *writeQueue) spoolID() (string, error) {
	name := filepath.Join(q.dir, "queue.id")
	data, err := os.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		id := rand.Text()
		return id, q.writeMarker(name, id)
	}
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > 64 {
		return "", errors.New("invalid local write queue ID")
	}
	return string(data), nil
}

func (q *writeQueue) writeMarker(name, value string) error {
	file, err := os.CreateTemp(q.dir, "marker-*")
	if err != nil {
		return err
	}
	_, err = file.WriteString(value)
	if err := q.seal(file, err); err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := os.Rename(file.Name(), name); err != nil {
		return err
	}
	return q.syncDirectory()
}

func decodeJob(record service.WriteRecord) (writeJob, error) {
	var job writeJob
	if err := json.Unmarshal([]byte(record.Payload), &job); err != nil {
		return job, err
	}
	if job.ID == "" || job.ID != record.ID {
		return job, errors.New("write queue contains an invalid job ID")
	}
	if job.Body != "" && (filepath.Base(job.Body) != job.Body || !strings.HasPrefix(job.Body, "upload-")) {
		return job, errors.New("write queue contains an invalid staged file name")
	}
	return job, nil
}

func (q *writeQueue) importLegacy(ctx context.Context) error {
	marker := filepath.Join(q.dir, "legacy.imported")
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	legacy := filepath.Join(q.dir, "queue.sqlite")
	if _, err := os.Stat(legacy); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	records, err := service.LegacyWriteRecords(ctx, legacy)
	if err != nil {
		return err
	}
	for _, record := range records {
		if _, exists := q.jobs[record.ID]; exists {
			continue
		}
		job, err := decodeJob(record)
		if err != nil {
			return err
		}
		if err := q.journal.Save(ctx, record); err != nil {
			return err
		}
		q.jobs[job.ID] = job
	}
	return q.writeMarker(marker, "1")
}

func (q *writeQueue) record(job writeJob) error {
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if err := q.updateJournal(func(ctx context.Context) error {
		return q.journal.Save(ctx, service.WriteRecord{ID: job.ID, Payload: string(raw)})
	}); err != nil {
		return err
	}
	q.jobs[job.ID] = job
	if !job.Attempted {
		q.report(job, "pending")
	}
	return nil
}

// A failed remote database request can have committed. Freeze the in-memory
// queue and retain staged bytes until a remount reads the authoritative state.
func (q *writeQueue) updateJournal(update func(context.Context) error) error {
	if q.journalErr != nil {
		return q.journalErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), journalTimeout)
	defer cancel()
	if err := update(ctx); err != nil {
		q.journalErr = errors.Join(errJournalUnavailable, err)
	}
	return q.journalErr
}

func (q *writeQueue) close() {
	if q.journal != nil {
		q.journal.Close()
	}
	if q.lock != nil {
		q.lock.Close()
	}
}
