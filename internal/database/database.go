package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens the SQLite file and applies pending migrations.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("locate user config directory: %w", err)
		}
		path = filepath.Join(config, "freesync", "freesync.sqlite")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	dsn := url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(path),
		RawQuery: "_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29&_txlock=immediate",
	}
	conn, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	conn.SetMaxOpenConns(1)
	if err := migrate(ctx, conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate SQLite database %q: %w", path, err)
	}
	return conn, nil
}

func migrate(ctx context.Context, conn *sql.DB) error {
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, files)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}
