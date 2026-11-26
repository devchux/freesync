package service

import (
	"context"
	"database/sql"

	"github.com/devchux/freesync/internal/database/dbsql"
)

type CatalogEntry struct {
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
}

type Catalog struct {
	db *sql.DB
}

func NewCatalog(db *sql.DB) *Catalog {
	return &Catalog{db: db}
}

// ReplaceEntries commits a complete scan for one folder atomically.
func (c *Catalog) ReplaceEntries(ctx context.Context, folder string, entries []CatalogEntry) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queries := dbsql.New(tx)
	if err := queries.DeleteCatalogEntries(ctx, folder); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := queries.InsertCatalogEntry(ctx, dbsql.InsertCatalogEntryParams{
			Folder:    folder,
			Path:      entry.Path,
			Directory: entry.Directory,
			Size:      entry.Size,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
