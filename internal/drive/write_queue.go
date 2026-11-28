package drive

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/devchux/freesync/internal/service"
	"github.com/devchux/freesync/internal/storage"
)

var (
	errWritePending       = errors.New("a write is already pending for this file")
	errJournalUnavailable = errors.New("write queue database is unavailable. Remount to recover pending writes")
)

type writeRemote interface {
	CommitWrite(context.Context, string, string, string, bool, io.ReadSeeker) error
	CommitDelete(context.Context, string, string) error
}

type writeJob struct {
	ID             string
	Key            string
	Revision       string
	Source         string
	SourceRevision string
	Body           string
	Size           int64
	Modified       time.Time
	Attempted      bool
	Blocked        bool
	Failures       int
	RetryAt        time.Time
}

type writeQueue struct {
	invalidate func()
	dir        string
	journal    *service.WriteJournal
	journalErr error
	lock       *os.File
	remote     writeRemote
	mu         sync.Mutex
	jobs       map[string]writeJob
	held       map[string]bool
}

// active returns a job that reserves key. The caller holds q.mu.
func (q *writeQueue) active(key string) (writeJob, bool) {
	for _, job := range q.jobs {
		if !job.Blocked && (job.Key == key || (job.Source != "" && job.Source == key)) {
			return job, true
		}
	}
	return writeJob{}, false
}

func (q *writeQueue) pending(key string) (writeJob, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.active(key)
}

// beginEdit reserves the key and opens an earlier unsent snapshot, if present.
func (q *writeQueue) beginEdit(key string) (*os.File, writeJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.journalErr != nil {
		return nil, writeJob{}, q.journalErr
	}
	job, ok := q.active(key)
	if !ok {
		q.held[key] = true
		return nil, writeJob{}, nil
	}
	if job.Key != key || job.Body == "" || job.Source != "" || job.Attempted {
		return nil, writeJob{}, errWritePending
	}
	file, err := os.Open(filepath.Join(q.dir, job.Body))
	if err == nil {
		q.held[key] = true
	}
	return file, job, err
}

func (q *writeQueue) endEdit(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.held, key)
	if job, ok := q.active(key); ok && job.Key == key && job.Source == "" && job.Revision == "" && !job.Attempted {
		// Give editors time to replace the destination with a newly closed temporary file.
		job.RetryAt = time.Now().Add(time.Second)
		q.jobs[job.ID] = job
	}
}

func (q *writeQueue) changes(dir string) []writeJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	var jobs []writeJob
	for _, job := range q.jobs {
		if !job.Blocked && (parentKey(job.Key) == dir || (job.Source != "" && parentKey(job.Source) == dir)) {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func parentKey(key string) string {
	index := strings.LastIndexByte(key, '/')
	if index < 0 {
		return ""
	}
	return key[:index]
}

func (q *writeQueue) enqueue(job writeJob) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.active(job.Key); exists {
		return errWritePending
	}
	if job.Source != "" {
		if _, exists := q.active(job.Source); exists {
			return errWritePending
		}
	}
	job.ID = rand.Text()
	return q.record(job)
}

// save queues an immutable snapshot. An edit can replace its own queued
// snapshot while that snapshot has yet to be sent to the provider.
func (q *writeQueue) save(key, revision, previous string, file *os.File) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.journalErr != nil {
		return "", q.journalErr
	}
	job, exists := q.active(key)
	if exists && (job.ID != previous || job.Attempted || job.Source != "") {
		return "", errWritePending
	}
	if !exists && previous != "" {
		// An earlier snapshot may already be remote. Reopening the file gets
		// a new remote revision before any further write.
		return "", errWritePending
	}
	newJob, err := q.snapshot(key, revision, file)
	if err != nil {
		return "", err
	}
	if exists {
		newJob.ID = job.ID
	}
	if err := q.record(newJob); err != nil {
		return "", fmt.Errorf("local upload retained at %s: %w", filepath.Join(q.dir, newJob.Body), err)
	}
	if exists {
		q.removeBody(job)
	}
	q.report(newJob, "pending")
	return newJob.ID, nil
}

func (q *writeQueue) snapshot(key, revision string, file *os.File) (writeJob, error) {
	info, err := file.Stat()
	if err != nil {
		return writeJob{}, err
	}
	body, err := os.CreateTemp(q.dir, "upload-*")
	if err != nil {
		return writeJob{}, err
	}
	_, copyErr := io.Copy(body, io.NewSectionReader(file, 0, info.Size()))
	if err := q.seal(body, copyErr); err != nil {
		return writeJob{}, err
	}
	return writeJob{ID: rand.Text(), Key: key, Revision: revision, Body: filepath.Base(body.Name()),
		Size: info.Size(), Modified: info.ModTime()}, nil
}

// seal makes a staged file durable before its name goes into the journal.
func (q *writeQueue) seal(body *os.File, err error) error {
	if err == nil {
		err = body.Sync()
	}
	err = errors.Join(err, body.Close())
	if err == nil {
		err = q.syncDirectory()
	}
	if err != nil {
		os.Remove(body.Name())
	}
	return err
}

func (q *writeQueue) syncDirectory() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(q.dir)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

// movePending handles an editor's temporary file before its first upload.
func (q *writeQueue) movePending(from, to, revision string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, err := q.unsentCreate(from)
	if err != nil {
		return err
	}
	if _, exists := q.active(to); exists {
		return errWritePending
	}
	job.Key, job.Revision = to, revision
	job.RetryAt = time.Time{}
	return q.record(job)
}

func (q *writeQueue) discardPending(key string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, err := q.unsentCreate(key)
	if err != nil {
		return err
	}
	return q.complete(job)
}

func (q *writeQueue) unsentCreate(key string) (writeJob, error) {
	job, ok := q.active(key)
	if !ok || job.Key != key || job.Body == "" || job.Source != "" || job.Revision != "" || job.Attempted {
		return writeJob{}, errWritePending
	}
	return job, nil
}

func (q *writeQueue) localFile(key string) (*os.File, writeJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.active(key)
	if !ok || job.Key != key || job.Body == "" {
		return nil, writeJob{}, fs.ErrNotExist
	}
	file, err := os.Open(filepath.Join(q.dir, job.Body))
	return file, job, err
}

func (q *writeQueue) complete(job writeJob) error {
	if err := q.updateJournal(func(ctx context.Context) error {
		return q.journal.Delete(ctx, job.ID)
	}); err != nil {
		return err
	}
	delete(q.jobs, job.ID)
	q.removeBody(job)
	return nil
}

func (q *writeQueue) removeBody(job writeJob) {
	if job.Body != "" {
		if err := os.Remove(filepath.Join(q.dir, job.Body)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("FreeSync could not remove staged upload %s: %v", job.Body, err)
		}
	}
}

func (q *writeQueue) run(ctx context.Context) {
	q.mu.Lock()
	for _, job := range q.jobs {
		state := "pending"
		if job.Blocked {
			state = "blocked"
		}
		q.report(job, state)
	}
	q.mu.Unlock()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		q.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (q *writeQueue) drain(ctx context.Context) {
	q.mu.Lock()
	if q.journalErr != nil {
		q.mu.Unlock()
		return
	}
	var ids []string
	now := time.Now()
	for id, job := range q.jobs {
		if !job.Blocked && !now.Before(job.RetryAt) && !q.held[job.Key] {
			ids = append(ids, id)
		}
	}
	q.mu.Unlock()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := q.process(ctx, id); err != nil {
			log.Printf("FreeSync queued operation %s: %v", id, err)
		}
	}
}

func (q *writeQueue) process(ctx context.Context, id string) error {
	q.mu.Lock()
	if q.journalErr != nil {
		q.mu.Unlock()
		return q.journalErr
	}
	job, ok := q.jobs[id]
	if !ok || job.Blocked || q.held[job.Key] {
		q.mu.Unlock()
		return nil
	}
	replay := job.Attempted
	if !replay {
		job.Attempted = true
		if err := q.record(job); err != nil {
			q.mu.Unlock()
			return err
		}
	}
	q.report(job, "uploading")
	q.mu.Unlock()

	// Permit large transfers on slower links without extending delete timeouts.
	timeout := 2*time.Minute + time.Duration(job.Size/(64<<10))*time.Second
	request, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := q.apply(request, job, replay)
	if q.invalidate != nil {
		q.invalidate()
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	switch {
	case err == nil:
		if err := q.complete(job); err != nil {
			return err
		}
		q.report(job, "committed")
		return nil
	case errors.Is(err, storage.ErrFileChanged), errors.Is(err, storage.ErrUploadTooLarge), errors.Is(err, fs.ErrPermission):
		job.Blocked = true
		if persistErr := q.record(job); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		state := "blocked"
		if errors.Is(err, storage.ErrFileChanged) {
			state = "conflicted"
		}
		q.report(job, state)
		if job.Body != "" {
			return fmt.Errorf("%q could not be committed. Local copy retained at %s: %w", job.Key, filepath.Join(q.dir, job.Body), err)
		}
		return fmt.Errorf("%q conflicts with a remote change: %w", job.Key, err)
	default:
		if ctx.Err() == nil {
			job.Failures++
			delay := min(time.Minute, (5*time.Second)<<min(job.Failures-1, 4))
			job.RetryAt = time.Now().Add(delay)
			if persistErr := q.record(job); persistErr != nil {
				return errors.Join(err, persistErr)
			}
		}
		q.report(job, "pending retry")
		return fmt.Errorf("%q will be retried: %w", job.Key, err)
	}
}

func (q *writeQueue) apply(ctx context.Context, job writeJob, replay bool) error {
	if job.Body == "" {
		return q.remote.CommitDelete(ctx, job.Key, job.Revision)
	}
	body, err := os.Open(filepath.Join(q.dir, job.Body))
	if err != nil {
		return err
	}
	defer body.Close()
	info, err := body.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != job.Size {
		return fmt.Errorf("staged upload %q has an unexpected type or size", job.Body)
	}
	if err := q.remote.CommitWrite(ctx, job.Key, job.Revision, job.ID, replay, body); err != nil {
		return err
	}
	if job.Source != "" {
		return q.remote.CommitDelete(ctx, job.Source, job.SourceRevision)
	}
	return nil
}

func (q *writeQueue) report(job writeJob, state string) {
	if job.Body != "" && q.invalidate != nil {
		log.Printf("FreeSync upload %s %q: %s", job.ID, job.Key, state)
	}
}
