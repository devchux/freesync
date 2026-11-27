package service

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/devchux/freesync/internal/database"
)

func TestWriteJournal(t *testing.T) {
	for _, engine := range []struct {
		name string
		env  string
	}{{database.SQLite, ""}, {database.PostgreSQL, "FREESYNC_TEST_POSTGRES"}, {database.MySQL, "FREESYNC_TEST_MYSQL"}} {
		t.Run(engine.name, func(t *testing.T) {
			cfg := database.Config{Type: engine.name}
			if engine.env == "" {
				cfg.Address = filepath.Join(t.TempDir(), "journal.sqlite")
			} else {
				cfg.Address = os.Getenv(engine.env)
				if cfg.Address == "" {
					t.Skip("set " + engine.env + " to run against a test database")
				}
			}
			testWriteJournal(t, cfg)
		})
	}
}

func testWriteJournal(t *testing.T, cfg database.Config) {
	t.Helper()
	ctx := context.Background()
	queueID := rand.Text()
	journal, err := OpenWriteJournal(ctx, cfg, queueID)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	record := WriteRecord{ID: "test-job", Payload: "original"}
	if err := journal.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	defer journal.Delete(ctx, record.ID)

	record.Payload = `{"file":"更新.txt"}`
	if err := journal.Save(ctx, record); err != nil {
		t.Fatal(err)
	}

	other, err := OpenWriteJournal(ctx, cfg, rand.Text())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if records, err := other.List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("unrelated spool saw records %v, error %v", records, err)
	}
	if records, err := journal.List(ctx); err != nil || len(records) != 1 || records[0] != record {
		t.Fatalf("queue records = %v, error %v", records, err)
	}
	if err := journal.Delete(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if records, err := journal.List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("records after deletion = %v, error %v", records, err)
	}
}
