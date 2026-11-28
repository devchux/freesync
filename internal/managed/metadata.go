package managed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/devchux/freesync/internal/storage"
)

// Backup rows use strings for a portable, lossless encoding of SQL values.
type metadata struct {
	Version int
	Drives  [][]string
	Members [][]string
	Files   [][]string
	Commits [][]string
}

type metadataTable struct {
	name    string
	columns string
	numeric int // Number of trailing integer columns.
	rows    *[][]string
}

func (m *metadata) tables() []metadataTable {
	return []metadataTable{
		{"managed_drives", "id", 0, &m.Drives},
		{"managed_members", "drive,name,credential,invited,writable", 2, &m.Members},
		{"managed_files", "drive,path,revision,size,modified", 2, &m.Files},
		{"managed_commits", "drive,member,id,path,base", 0, &m.Commits},
	}
}

func readMetadata(ctx context.Context, db *sql.DB) (metadata, error) {
	result := metadata{Version: 1}
	for _, table := range result.tables() {
		records, err := readMetadataTable(ctx, db, table)
		if err != nil {
			return metadata{}, err
		}
		*table.rows = records
	}
	return result, nil
}

func readMetadataTable(ctx context.Context, db *sql.DB, table metadataTable) ([][]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+table.columns+" FROM "+table.name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	count := strings.Count(table.columns, ",") + 1
	var records [][]string
	for rows.Next() {
		record, fields := make([]string, count), make([]any, count)
		for i := range record {
			fields[i] = &record[i]
		}
		if err := rows.Scan(fields...); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (m *metadata) contents() ([]storage.FileInfo, error) {
	var files []storage.FileInfo
	for _, row := range m.Files {
		if len(row) != 5 {
			return nil, errors.New("invalid backup file record")
		}
		size, err := strconv.ParseInt(row[3], 10, 64)
		if err != nil || size < 0 || !validPath(row[2]) || strings.Contains(row[2], "/") {
			return nil, errors.New("invalid backup revision")
		}
		files = append(files, storage.FileInfo{Revision: row[2], Object: storage.Object{Size: size}})
	}
	return files, nil
}

func (m *metadata) insert(ctx context.Context, tx *sql.Tx, databaseType string) error {
	for _, table := range m.tables() {
		count := strings.Count(table.columns, ",") + 1
		placeholders := strings.TrimSuffix(strings.Repeat("?,", count), ",")
		query := bind(databaseType, "INSERT INTO "+table.name+" ("+table.columns+") VALUES ("+placeholders+")")
		for _, row := range *table.rows {
			if len(row) != count {
				return fmt.Errorf("invalid backup row for %s", table.name)
			}
			args := make([]any, count)
			for i, value := range row {
				if i < count-table.numeric {
					args[i] = value
					continue
				}
				number, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return err
				}
				args[i] = number
			}
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
	}
	return nil
}
