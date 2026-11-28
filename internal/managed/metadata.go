package managed

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/storage"
)

// Backup rows use strings for a portable, lossless encoding of SQL values.
type metadata struct {
	Version  int
	Drives   [][]string
	Members  [][]string
	Files    [][]string
	Commits  [][]string
	Accounts [][]string
	Owners   [][]string
	Grants   [][]string
}

func readMetadata(ctx context.Context, queries database.ManagedQueries) (metadata, error) {
	result := metadata{Version: 2}
	var err error
	result.Drives, err = readBackupRows(ctx, queries.AllManagedDrives, func(id string) []string {
		return []string{id}
	})
	if err != nil {
		return metadata{}, err
	}
	result.Accounts, err = readBackupRows(ctx, queries.AllManagedAccounts, func(row database.ManagedAccount) []string {
		return []string{row.ID, row.Email, row.FullName, row.PasswordHash}
	})
	if err != nil {
		return metadata{}, err
	}
	result.Owners, err = readBackupRows(ctx, queries.AllManagedOwners, func(row database.ManagedOwner) []string {
		return []string{row.Drive, row.AccountID}
	})
	if err != nil {
		return metadata{}, err
	}
	result.Grants, err = readBackupRows(ctx, queries.AllManagedGrants, func(row database.ManagedGrant) []string {
		return []string{row.Drive, row.AccountID, decimal(row.Pending), decimal(row.Writable)}
	})
	if err != nil {
		return metadata{}, err
	}
	result.Members, err = readBackupRows(ctx, queries.AllManagedLegacyMembers, func(row database.ManagedMember) []string {
		return []string{row.Drive, row.Name, row.Credential, decimal(row.Invited), decimal(row.Writable)}
	})
	if err != nil {
		return metadata{}, err
	}
	result.Files, err = readBackupRows(ctx, queries.AllManagedFiles, func(row database.ManagedFile) []string {
		return []string{row.Drive, row.Path, row.Revision, decimal(row.Size), decimal(row.Modified)}
	})
	if err != nil {
		return metadata{}, err
	}
	result.Commits, err = readBackupRows(ctx, queries.AllManagedCommits, func(row database.ManagedCommit) []string {
		return []string{row.Drive, row.Member, row.ID, row.Path, row.Base}
	})
	return result, err
}

func readBackupRows[T any](ctx context.Context, query func(context.Context) ([]T, error), encode func(T) []string) ([][]string, error) {
	rows, err := query(ctx)
	if err != nil {
		return nil, err
	}
	var records [][]string
	for _, row := range rows {
		records = append(records, encode(row))
	}
	return records, nil
}

func decimal(value int64) string { return strconv.FormatInt(value, 10) }

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

func (m *metadata) insert(ctx context.Context, queries database.ManagedQueries) error {
	// Insert in foreign key order. The caller rolls back on any malformed row.
	tables := []struct {
		name   string
		rows   [][]string
		width  int
		insert func([]string) error
	}{
		{"drives", m.Drives, 1, func(row []string) error {
			return queries.InsertManagedDrive(ctx, row[0])
		}},
		{"accounts", m.Accounts, 4, func(row []string) error {
			return queries.InsertManagedAccount(ctx, row[0], row[1], row[2], row[3])
		}},
		{"owners", m.Owners, 2, func(row []string) error {
			return queries.InsertManagedOwner(ctx, row[0], row[1])
		}},
		{"grants", m.Grants, 4, func(row []string) error {
			return insertBackupWithIntegers(row, func(pending, writable int64) error {
				return queries.InsertManagedGrant(ctx, row[0], row[1], pending, writable)
			})
		}},
		{"legacy members", m.Members, 5, func(row []string) error {
			return insertBackupWithIntegers(row, func(invited, writable int64) error {
				return queries.InsertManagedLegacyMember(ctx, row[0], row[1], row[2], invited, writable)
			})
		}},
		{"files", m.Files, 5, func(row []string) error {
			return insertBackupWithIntegers(row, func(size, modified int64) error {
				return queries.InsertManagedFile(ctx, row[0], row[1], row[2], size, modified)
			})
		}},
		{"commits", m.Commits, 5, func(row []string) error {
			return queries.InsertManagedCommit(ctx, row[0], row[1], row[2], row[3], row[4])
		}},
	}
	for _, table := range tables {
		for _, row := range table.rows {
			if len(row) != table.width {
				return fmt.Errorf("invalid backup row for %s", table.name)
			}
			if err := table.insert(row); err != nil {
				return fmt.Errorf("restore %s: %w", table.name, err)
			}
		}
	}
	return nil
}

func insertBackupWithIntegers(row []string, insert func(int64, int64) error) error {
	first, err := strconv.ParseInt(row[len(row)-2], 10, 64)
	if err != nil {
		return err
	}
	second, err := strconv.ParseInt(row[len(row)-1], 10, 64)
	if err != nil {
		return err
	}
	return insert(first, second)
}
