package drive

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/storage"
)

func openTestQueue(dir string, remote writeRemote) (*writeQueue, error) {
	config := database.Config{Type: database.SQLite, Address: filepath.Join(dir, "test.sqlite")}
	return openWriteQueue(dir, remote, config)
}

type testObject struct {
	body     string
	revision string
	commitID string
}

type testRemote struct {
	files       map[string]testObject
	writeError  error
	deleteError error
	commits     int
}

func (r *testRemote) CommitWrite(_ context.Context, key, revision, commitID string, replay bool, body io.ReadSeeker) error {
	if r.writeError != nil {
		return r.writeError
	}
	current := r.files[key]
	if replay && current.commitID == commitID {
		return nil
	}
	if current.revision != revision {
		return storage.ErrFileChanged
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	r.commits++
	r.files[key] = testObject{body: string(data), revision: commitID, commitID: commitID}
	return nil
}

func (r *testRemote) CommitDelete(_ context.Context, key, revision string) error {
	if r.deleteError != nil {
		return r.deleteError
	}
	if file, exists := r.files[key]; exists && file.revision != revision {
		return storage.ErrFileChanged
	}
	delete(r.files, key)
	return nil
}

func stageTestWrite(t *testing.T, q *writeQueue, key, revision, text string) writeJob {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "source-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
	job, err := q.snapshot(key, revision, file)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestWriteQueueRecoversAfterRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	offline := errors.New("offline")
	remote := &testRemote{files: make(map[string]testObject), writeError: offline}
	queue, err := openTestQueue(dir, remote)
	if err != nil {
		t.Fatal(err)
	}
	if another, err := openTestQueue(dir, remote); err == nil {
		another.close()
		queue.close()
		t.Fatal("a second queue acquired the same journal")
	}
	job := stageTestWrite(t, queue, "report.txt", "", "saved while offline")
	if err := queue.enqueue(job); err != nil {
		queue.close()
		t.Fatal(err)
	}
	queued, _ := queue.pending(job.Key)
	if err := queue.process(ctx, queued.ID); !errors.Is(err, offline) {
		queue.close()
		t.Fatalf("offline upload = %v", err)
	}
	queue.close()

	remote.writeError = nil
	queue, err = openTestQueue(dir, remote)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.close()
	if err := queue.process(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	if remote.files[job.Key].body != "saved while offline" {
		t.Fatalf("replayed remote file = %+v", remote.files[job.Key])
	}
	if _, pending := queue.pending(job.Key); pending {
		t.Fatal("completed upload is still queued")
	}
	if _, err := os.Stat(filepath.Join(dir, job.Body)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed upload left a staged copy: %v", err)
	}
}

func TestWriteQueueRenameRecoversBetweenCopyAndDelete(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	offline := errors.New("delete interrupted")
	remote := &testRemote{files: map[string]testObject{"old": {body: "contents", revision: "original"}}, deleteError: offline}
	queue, err := openTestQueue(dir, remote)
	if err != nil {
		t.Fatal(err)
	}
	job := stageTestWrite(t, queue, "new", "", "contents")
	job.Source, job.SourceRevision = "old", "original"
	if err := queue.enqueue(job); err != nil {
		queue.close()
		t.Fatal(err)
	}
	queued, _ := queue.pending("new")
	if err := queue.process(ctx, queued.ID); !errors.Is(err, offline) {
		queue.close()
		t.Fatalf("interrupted rename = %v", err)
	}
	if remote.files["old"].body != "contents" || remote.files["new"].body != "contents" {
		queue.close()
		t.Fatal("rename did not retain the source until deletion")
	}
	queue.close()

	remote.deleteError = nil
	queue, err = openTestQueue(dir, remote)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.close()
	if err := queue.process(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	if _, exists := remote.files["old"]; exists || remote.commits != 1 {
		t.Fatalf("rename replay state = %+v with %d upload commits", remote.files, remote.commits)
	}
}

func TestWriteQueueKeepsConflictingEditLocal(t *testing.T) {
	ctx := context.Background()
	remote := &testRemote{files: map[string]testObject{"file": {body: "newer remote", revision: "new"}}}
	queue, err := openTestQueue(t.TempDir(), remote)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.close()
	job := stageTestWrite(t, queue, "file", "old", "local edit")
	if err := queue.enqueue(job); err != nil {
		t.Fatal(err)
	}
	queued, _ := queue.pending(job.Key)
	if err := queue.process(ctx, queued.ID); !errors.Is(err, storage.ErrFileChanged) {
		t.Fatalf("stale upload = %v", err)
	}
	if !queue.jobs[queued.ID].Blocked || remote.files["file"].body != "newer remote" {
		t.Fatal("conflict did not preserve the remote file and queue record")
	}
	local, err := os.ReadFile(filepath.Join(queue.dir, job.Body))
	if err != nil || string(local) != "local edit" {
		t.Fatalf("retained local data = %q, %v", local, err)
	}
}

func TestFreshManagedSpool(t *testing.T) {
	dir := t.TempDir()
	config := database.Config{Type: database.SQLite, Address: filepath.Join(dir, "queue.sqlite")}
	queue, err := openWriteQueue(dir, &testRemote{}, config)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.close()
}

func TestImportPreviousLocalJournal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	previous, err := database.Open(ctx, database.Config{Type: database.SQLite, Address: filepath.Join(dir, "queue.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer previous.Close()
	if _, err := previous.Exec("CREATE TABLE writes (id TEXT PRIMARY KEY, job BLOB NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := previous.Exec("INSERT INTO writes VALUES (?, ?)", "legacy-job",
		`{"ID":"legacy-job","Key":"report","Body":"upload-legacy","Size":5}`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "upload-legacy"), []byte("saved"), 0o600); err != nil {
		t.Fatal(err)
	}

	remote := &testRemote{files: make(map[string]testObject)}
	queue, err := openTestQueue(dir, remote)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.process(ctx, "legacy-job"); err != nil {
		queue.close()
		t.Fatal(err)
	}
	queue.close()
	if remote.files["report"].body != "saved" {
		t.Fatalf("imported file = %+v", remote.files["report"])
	}
	queue, err = openTestQueue(dir, remote)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.close()
	if _, pending := queue.pending("report"); pending {
		t.Fatal("reopening imported a completed legacy job again")
	}
}

func TestJournalFailureKeepsStagedBytes(t *testing.T) {
	dir := t.TempDir()
	queue, err := openTestQueue(dir, &testRemote{})
	if err != nil {
		t.Fatal(err)
	}
	defer queue.close()
	if err := queue.journal.Close(); err != nil {
		t.Fatal(err)
	}
	draft, err := os.CreateTemp(dir, "edit-*")
	if err != nil {
		t.Fatal(err)
	}
	defer draft.Close()
	if _, err := draft.WriteString("local changes"); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.save("file", "", "", draft); !errors.Is(err, errJournalUnavailable) {
		t.Fatalf("queue save error = %v", err)
	}
	bodies, err := filepath.Glob(filepath.Join(dir, "upload-*"))
	if err != nil || len(bodies) != 1 {
		t.Fatalf("staged files = %v, error %v", bodies, err)
	}
	content, err := os.ReadFile(bodies[0])
	if err != nil || string(content) != "local changes" {
		t.Fatalf("staged body = %q, error %v", content, err)
	}
	if _, _, err := queue.beginEdit("other"); !errors.Is(err, errJournalUnavailable) {
		t.Fatalf("further writes after a journal error = %v", err)
	}
}
