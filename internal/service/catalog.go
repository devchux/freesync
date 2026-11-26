package service

import (
	"context"
	"database/sql"

	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/database/mysql/mysqlsql"
	"github.com/devchux/freesync/internal/database/postgres/pgsql"
	"github.com/devchux/freesync/internal/database/sqlite/dbsql"
)

type CatalogEntry struct {
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
}

type Catalog struct {
	db      *sql.DB
	backend string
}

func NewCatalog(db *sql.DB, backend string) *Catalog {
	return &Catalog{db: db, backend: backend}
}

// ReplaceEntries commits a complete scan for one folder atomically.
func (c *Catalog) ReplaceEntries(ctx context.Context, folder string, entries []CatalogEntry) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var deleteEntries func(context.Context, string) error
	var insertEntry func(context.Context, dbsql.InsertCatalogEntryParams) error
	switch c.backend {
	case database.PostgreSQL:
		queries := pgsql.New(tx)
		deleteEntries = queries.DeleteCatalogEntries
		insertEntry = func(ctx context.Context, row dbsql.InsertCatalogEntryParams) error {
			return queries.InsertCatalogEntry(ctx, pgsql.InsertCatalogEntryParams(row))
		}
	case database.MySQL:
		queries := mysqlsql.New(tx)
		deleteEntries = queries.DeleteCatalogEntries
		insertEntry = func(ctx context.Context, row dbsql.InsertCatalogEntryParams) error {
			return queries.InsertCatalogEntry(ctx, mysqlsql.InsertCatalogEntryParams{
				Folder: []byte(row.Folder), Path: []byte(row.Path), Directory: row.Directory, Size: row.Size,
			})
		}
	default:
		queries := dbsql.New(tx)
		deleteEntries = queries.DeleteCatalogEntries
		insertEntry = queries.InsertCatalogEntry
	}

	if err := deleteEntries(ctx, folder); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := insertEntry(ctx, dbsql.InsertCatalogEntryParams{
			Folder: folder, Path: entry.Path, Directory: entry.Directory, Size: entry.Size,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
