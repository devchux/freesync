package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const (
	SQLite     = "sqlite"
	PostgreSQL = "postgres"
	MySQL      = "mysql"
)

type Config struct {
	Type    string `json:"type"`
	Address string `json:"address,omitempty"`
}

func ParseType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "sqlite":
		return SQLite, nil
	case "postgres", "postgresql":
		return PostgreSQL, nil
	case "mysql":
		return MySQL, nil
	default:
		return "", errors.New("choose SQLite, PostgreSQL, or MySQL")
	}
}

//go:embed migrations/*.sql postgres/migrations/*.sql mysql/migrations/*.sql
var migrations embed.FS

// Open connects to the selected database and applies pending migrations.
func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	driver, dialect, dir := "sqlite", goose.DialectSQLite3, "migrations"
	dsn := cfg.Address
	switch cfg.Type {
	case SQLite:
		var err error
		dsn, err = sqliteDSN(dsn)
		if err != nil {
			return nil, err
		}
	case PostgreSQL:
		driver, dialect, dir = "pgx", goose.DialectPostgres, "postgres/migrations"
	case MySQL:
		driver, dialect, dir = "mysql", goose.DialectMySQL, "mysql/migrations"
	default:
		return nil, errors.New("unsupported database type")
	}
	if dsn == "" {
		return nil, fmt.Errorf("%s requires a connection string", cfg.Type)
	}

	conn, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid %s connection settings", cfg.Type)
	}
	if cfg.Type == SQLite {
		conn.SetMaxOpenConns(1)
	}
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		if cfg.Type != SQLite {
			return nil, fmt.Errorf("connect to %s: check the connection string and server availability", cfg.Type)
		}
		return nil, fmt.Errorf("connect to SQLite: %w", err)
	}
	if err := migrate(ctx, conn, dialect, dir); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate %s database: %w", cfg.Type, err)
	}
	return conn, nil
}

func sqliteDSN(path string) (string, error) {
	if path == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("locate user config directory: %w", err)
		}
		path = filepath.Join(config, "freesync", "freesync.sqlite")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create database directory: %w", err)
	}
	dsn := url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(path),
		RawQuery: "_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29&_txlock=immediate",
	}
	return dsn.String(), nil
}

func migrate(ctx context.Context, conn *sql.DB, dialect goose.Dialect, dir string) error {
	files, err := fs.Sub(migrations, dir)
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(dialect, conn, files)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}
