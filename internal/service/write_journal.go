package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/devchux/freesync/internal/database"

	"github.com/devchux/freesync/internal/database/mysql/mysqlsql"
	"github.com/devchux/freesync/internal/database/postgres/pgsql"
	"github.com/devchux/freesync/internal/database/sqlite/dbsql"
	"github.com/devchux/freesync/internal/database/sqlite/legacy/legacydbsql"
)

// WriteJournal stores queue metadata. Its queue ID belongs to the local spool,
// so clients sharing a database cannot replay one another's staged files.
type WriteJournal struct {
	db      *sql.DB
	dialect string
	queueID string
}

type WriteRecord struct {
	ID      string
	Payload string
}

func OpenWriteJournal(ctx context.Context, config database.Config, queueID string) (*WriteJournal, error) {
	db, err := database.Open(ctx, config)
	if err != nil {
		return nil, err
	}
	return &WriteJournal{db: db, dialect: config.Type, queueID: queueID}, nil
}

func (j *WriteJournal) Close() error {
	return j.db.Close()
}

func (j *WriteJournal) List(ctx context.Context) ([]WriteRecord, error) {
	var records []WriteRecord
	switch j.dialect {
	case database.PostgreSQL:
		rows, err := pgsql.New(j.db).ListWriteJobs(ctx, j.queueID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			records = append(records, WriteRecord{ID: row.JobID, Payload: row.Payload})
		}
	case database.MySQL:
		rows, err := mysqlsql.New(j.db).ListWriteJobs(ctx, j.queueID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			records = append(records, WriteRecord{ID: row.JobID, Payload: string(row.Payload)})
		}
	default:
		rows, err := dbsql.New(j.db).ListWriteJobs(ctx, j.queueID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			records = append(records, WriteRecord{ID: row.JobID, Payload: row.Payload})
		}
	}
	return records, nil
}

func (j *WriteJournal) Save(ctx context.Context, record WriteRecord) error {
	switch j.dialect {
	case database.PostgreSQL:
		return pgsql.New(j.db).SaveWriteJob(ctx, pgsql.SaveWriteJobParams{
			QueueID: j.queueID,
			JobID:   record.ID,
			Payload: record.Payload,
		})
	case database.MySQL:
		return mysqlsql.New(j.db).SaveWriteJob(ctx, mysqlsql.SaveWriteJobParams{
			QueueID: j.queueID,
			JobID:   record.ID,
			Payload: []byte(record.Payload),
		})
	default:
		return dbsql.New(j.db).SaveWriteJob(ctx, dbsql.SaveWriteJobParams{
			QueueID: j.queueID,
			JobID:   record.ID,
			Payload: record.Payload,
		})
	}
}

func (j *WriteJournal) Delete(ctx context.Context, id string) error {
	switch j.dialect {
	case database.PostgreSQL:
		return pgsql.New(j.db).DeleteWriteJob(ctx, pgsql.DeleteWriteJobParams{
			QueueID: j.queueID,
			JobID:   id,
		})
	case database.MySQL:
		return mysqlsql.New(j.db).DeleteWriteJob(ctx, mysqlsql.DeleteWriteJobParams{
			QueueID: j.queueID,
			JobID:   id,
		})
	default:
		return dbsql.New(j.db).DeleteWriteJob(ctx, dbsql.DeleteWriteJobParams{
			QueueID: j.queueID,
			JobID:   id,
		})
	}
}

// LegacyWriteRecords reads the journal written by the initial writable-mount
// implementation. It is used only when importing an existing local spool.
func LegacyWriteRecords(ctx context.Context, path string) ([]WriteRecord, error) {
	db, err := database.Open(ctx, database.Config{Type: database.SQLite, Address: path})
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := legacydbsql.New(db).ListLegacyWriteJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("read previous write journal: %w", err)
	}
	var records []WriteRecord
	for _, row := range rows {
		records = append(records, WriteRecord{ID: row.ID, Payload: string(row.Job)})
	}
	return records, nil
}
