package drive

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/managed"
	"github.com/devchux/freesync/internal/route"
	"github.com/devchux/freesync/internal/storage"
)

type managedTestContents map[string][]byte

func (m managedTestContents) New(_ context.Context, key string, data io.ReadSeeker) error {
	if _, exists := m[key]; exists {
		return fs.ErrExist
	}
	body, err := io.ReadAll(data)
	if err == nil {
		m[key] = body
	}
	return err
}
func (m managedTestContents) Delete(_ context.Context, key string) error {
	delete(m, key)
	return nil
}
func (m managedTestContents) Stat(_ context.Context, key string) (storage.FileInfo, error) {
	body, exists := m[key]
	if !exists {
		return storage.FileInfo{}, fs.ErrNotExist
	}
	return storage.FileInfo{Object: storage.Object{Key: key, Size: int64(len(body))}}, nil
}
func (m managedTestContents) ReadRange(_ context.Context, file storage.FileInfo, offset int64, dst []byte) (int, error) {
	return copy(dst, m[file.Key][offset:]), nil
}

func TestManagedDriveConflictAndRestore(t *testing.T) {
	ctx := t.Context()
	const adminToken = "managed-test-administrator"
	testConfig := func(env, filename string) database.Config {
		t.Helper()
		address := os.Getenv(env)
		if address == "" {
			return database.Config{Type: database.SQLite, Address: filepath.Join(t.TempDir(), filename)}
		}
		kind, err := database.ParseType(os.Getenv("FREESYNC_TEST_DB_TYPE"))
		if err != nil {
			t.Fatal(err)
		}
		return database.Config{Type: kind, Address: address}
	}
	serve := func(config database.Config, contents managedTestContents) string {
		t.Helper()
		db, err := database.Open(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		server := httptest.NewServer(route.Managed(&managed.Server{DB: db, DatabaseType: config.Type, Contents: contents, AdminToken: adminToken}))
		t.Cleanup(server.Close)
		return server.URL
	}
	client := func(endpoint, token string) *managed.Client {
		t.Helper()
		c, err := managed.NewClient(endpoint, "team", token)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	request := func(c *managed.Client, method, action string, values url.Values) io.ReadCloser {
		t.Helper()
		response, err := c.Request(ctx, method, action, values, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Close() })
		return response
	}
	assertCurrent := func(c *managed.Client, want string) storage.FileInfo {
		t.Helper()
		entries, err := c.ReadDir(ctx, "")
		if err != nil || len(entries) != 1 || entries[0].Name != "report.txt" {
			t.Fatalf("listing = %+v, %v", entries, err)
		}
		file, err := c.Stat(ctx, "report.txt")
		if err != nil {
			t.Fatal(err)
		}
		body := make([]byte, file.Size)
		if _, err := c.ReadRange(ctx, file, 0, body); err != nil || string(body) != want {
			t.Fatalf("read = %q, %v, want %q", body, err, want)
		}
		return file
	}

	contents := make(managedTestContents)
	endpoint := serve(testConfig("FREESYNC_TEST_SOURCE_DB", "metadata.sqlite"), contents)
	admin := client(endpoint, adminToken)
	const password = "test account password"
	login := func(endpoint, email string) (managed.Account, *managed.Client) {
		t.Helper()
		account, token, err := managed.Login(ctx, endpoint, email, password)
		if err != nil {
			t.Fatal(err)
		}
		return account, client(endpoint, token)
	}
	const aliceEmail, bobEmail, readerEmail = "alice@example.test", "bob@example.test", "reader@example.test"
	for name, email := range map[string]string{"Alice Owner": aliceEmail, "Bob Writer": bobEmail, "Read Only": readerEmail} {
		if err := managed.SignUp(ctx, endpoint, name, email, password); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := managed.Login(ctx, endpoint, aliceEmail, "incorrect password"); err == nil {
		t.Fatal("incorrect password accepted")
	}
	aliceAccount, alice := login(endpoint, aliceEmail)
	_, bob := login(endpoint, bobEmail)
	_, reader := login(endpoint, readerEmail)
	request(alice, http.MethodPost, "create", nil)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	browser := &http.Client{Jar: jar}
	browserRequest := func(method, path, origin, body string, want int) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, endpoint+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := browser.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		if response.StatusCode != want {
			t.Fatalf("%s %s: got HTTP %d, want %d", method, path, response.StatusCode, want)
		}
		return response
	}
	signin := browserRequest(http.MethodPost, "/auth/login", endpoint, `{"email":"alice@example.test","password":"test account password"}`, http.StatusOK)
	cookies := signin.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("browser session cookie flags: %+v", cookies)
	}
	browserRequest(http.MethodGet, "/auth/me", "", "", http.StatusOK)
	list := browserRequest(http.MethodGet, "/drives", "", "", http.StatusOK)
	var drives []struct {
		ID    string
		Owner bool
	}
	if err := json.NewDecoder(list.Body).Decode(&drives); err != nil || len(drives) != 1 || drives[0].ID != "team" || !drives[0].Owner {
		t.Fatalf("browser ownership listing: %+v, %v", drives, err)
	}
	browserRequest(http.MethodPost, "/drives/forged/create", "https://other.example.test", "", http.StatusForbidden)
	browserRequest(http.MethodPost, "/auth/logout", endpoint, "", http.StatusNoContent)
	browserRequest(http.MethodGet, "/auth/me", "", "", http.StatusUnauthorized)
	if _, err := admin.Request(ctx, http.MethodGet, "access", nil, nil); !errors.Is(err, managed.ErrSessionExpired) {
		t.Fatalf("admin credential grants drive access: %v", err)
	}
	invite := func(email, writable string, member *managed.Client) {
		t.Helper()
		request(alice, http.MethodPost, "invite", url.Values{"email": {email}, "write": {writable}})
		if err := member.CheckAccess(ctx); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("pending invitation grants access: %v", err)
		}
		request(member, http.MethodPost, "accept", nil)
	}
	invite(bobEmail, "true", bob)
	invite(readerEmail, "false", reader)
	if _, err := bob.Request(ctx, http.MethodPost, "invite", url.Values{"email": {readerEmail}}, nil); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("non-owner can invite: %v", err)
	}
	if _, err := reader.Request(ctx, http.MethodPost, "revoke", url.Values{"email": {bobEmail}}, nil); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("non-owner can revoke: %v", err)
	}

	if err := reader.CheckAccess(ctx); err != nil || reader.WritableMount() {
		t.Fatalf("reader permissions: %v", err)
	}
	if err := reader.CommitWrite(ctx, "report.txt", "", "reader", false, strings.NewReader("denied")); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("reader save: %v", err)
	}
	const nested = "résumé/notes.txt"
	if err := alice.CommitWrite(ctx, nested, "", "nested", false, strings.NewReader("nested")); err != nil {
		t.Fatal(err)
	}
	entries, err := alice.ReadDir(ctx, "résumé")
	if err != nil || len(entries) != 1 || entries[0].Name != "notes.txt" {
		t.Fatalf("nested listing: %+v, %v", entries, err)
	}
	for _, collision := range []string{"résumé", nested + "/child.txt"} {
		if err := alice.CommitWrite(ctx, collision, "", "collision", false, strings.NewReader("denied")); !errors.Is(err, storage.ErrFileChanged) {
			t.Fatalf("file/directory collision at %q: %v", collision, err)
		}
	}
	nestedFile, err := alice.Stat(ctx, nested)
	if err != nil {
		t.Fatal(err)
	}
	if err := alice.CommitDelete(ctx, nested, nestedFile.Revision); err != nil {
		t.Fatal(err)
	}
	if err := alice.CommitWrite(ctx, "report.txt", "", "initial", false, strings.NewReader("initial")); err != nil {
		t.Fatal(err)
	}
	base := assertCurrent(bob, "initial")
	assertCurrent(alice, "initial")
	stage := func(c *managed.Client, text string) (*writeQueue, writeJob) {
		t.Helper()
		if err := c.CheckAccess(ctx); err != nil || !c.WritableMount() {
			t.Fatalf("writer permissions: %v", err)
		}
		q, err := openTestQueue(t.TempDir(), c)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(q.close)
		job := stageTestWrite(t, q, base.Key, base.Revision, text)
		if err := q.enqueue(job); err != nil {
			t.Fatal(err)
		}
		queued, ok := q.pending(base.Key)
		if !ok {
			t.Fatal("save was not staged")
		}
		return q, queued
	}
	aliceQueue, aliceJob := stage(alice, "Alice's edit")
	bobQueue, bobJob := stage(bob, "Bob's edit")
	if err := aliceQueue.process(ctx, aliceJob.ID); err != nil {
		t.Fatal(err)
	}
	if err := bobQueue.process(ctx, bobJob.ID); !errors.Is(err, storage.ErrFileChanged) {
		t.Fatalf("Bob save: %v", err)
	}
	assertCurrent(reader, "Alice's edit")
	pending, ok := bobQueue.jobs[bobJob.ID]
	body, err := os.ReadFile(filepath.Join(bobQueue.dir, pending.Body))
	if !ok || !pending.Blocked || err != nil || string(body) != "Bob's edit" {
		t.Fatalf("conflicted edit = %q, blocked = %t, err = %v", body, pending.Blocked, err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected only current contents, got %d", len(contents))
	}
	export := tar.NewReader(request(reader, http.MethodGet, "export", nil))
	header, err := export.Next()
	if err != nil || header.Name != base.Key {
		t.Fatalf("export header: %v, %v", header, err)
	}
	exported, err := io.ReadAll(export)
	if err != nil || string(exported) != "Alice's edit" {
		t.Fatalf("export body: %q, %v", exported, err)
	}
	request(alice, http.MethodPost, "revoke", url.Values{"email": {bobEmail}})
	if _, err := bob.Stat(ctx, base.Key); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("revoked member stat: %v", err)
	}
	backup := request(admin, http.MethodGet, "backup", nil)
	target := testConfig("FREESYNC_TEST_RESTORE_DB", "restored.sqlite")
	restoredContents := make(managedTestContents)
	if err := managed.Restore(ctx, target, restoredContents, backup); err != nil {
		t.Fatal(err)
	}
	retry := request(admin, http.MethodGet, "backup", nil)
	if err := managed.Restore(ctx, database.Config{Type: database.SQLite, Address: filepath.Join(t.TempDir(), "retry.sqlite")}, restoredContents, retry); err != nil {
		t.Fatalf("restore with matching contents already uploaded: %v", err)
	}
	restored := serve(target, restoredContents)
	restoredAccount, restoredAlice := login(restored, aliceEmail)
	if restoredAccount != aliceAccount {
		t.Fatalf("restored owner mismatch: %+v", restoredAccount)
	}
	request(restoredAlice, http.MethodGet, "members", nil)
	current := assertCurrent(restoredAlice, "Alice's edit")
	if current.Revision == base.Revision {
		t.Fatal("save did not advance revision")
	}
	_, restoredBob := login(restored, bobEmail)
	if err := restoredBob.CheckAccess(ctx); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("revoked member access: %v", err)
	}
	_, restoredReader := login(restored, readerEmail)
	if err := restoredReader.CheckAccess(ctx); err != nil || restoredReader.WritableMount() {
		t.Fatalf("restored reader permissions: %v", err)
	}
}
