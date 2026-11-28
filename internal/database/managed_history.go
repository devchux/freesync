package database

import (
	"context"

	"github.com/devchux/freesync/internal/database/mysql/mysqlsql"
	"github.com/devchux/freesync/internal/database/postgres/pgsql"
	"github.com/devchux/freesync/internal/database/sqlite/dbsql"
)

type ManagedHistorySetting = dbsql.ManagedHistorySetting
type ManagedVersion = dbsql.ManagedVersion

func mysqlHistorySetting(row mysqlsql.ManagedHistorySetting) ManagedHistorySetting {
	return ManagedHistorySetting{Drive: string(row.Drive), MaxAgeDays: row.MaxAgeDays, MaxVersions: row.MaxVersions}
}

func mysqlVersion(row mysqlsql.ManagedVersion) ManagedVersion {
	return ManagedVersion{Drive: string(row.Drive), Path: string(row.Path), Revision: string(row.Revision), Size: row.Size, Modified: row.Modified, Retired: row.Retired}
}

func (q ManagedQueries) AllManagedHistorySettings(ctx context.Context) ([]ManagedHistorySetting, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).AllManagedHistorySettings(ctx)
		return convertManagedRows(rows, err, func(v pgsql.ManagedHistorySetting) ManagedHistorySetting { return ManagedHistorySetting(v) })
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedHistorySettings(ctx)
		return convertManagedRows(rows, err, mysqlHistorySetting)
	default:
		return dbsql.New(q.db).AllManagedHistorySettings(ctx)
	}
}

func (q ManagedQueries) ManagedHistorySettingsByDrive(ctx context.Context, drive string) (ManagedHistorySetting, error) {
	switch q.dialect {
	case PostgreSQL:
		row, err := pgsql.New(q.db).ManagedHistorySettingsByDrive(ctx, drive)
		return ManagedHistorySetting(row), err
	case MySQL:
		row, err := mysqlsql.New(q.db).ManagedHistorySettingsByDrive(ctx, []byte(drive))
		return mysqlHistorySetting(row), err
	default:
		return dbsql.New(q.db).ManagedHistorySettingsByDrive(ctx, drive)
	}
}

func (q ManagedQueries) InsertManagedHistorySettings(ctx context.Context, drive string, maxAgeDays int64, maxVersions int64) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedHistorySettings(ctx, pgsql.InsertManagedHistorySettingsParams{Drive: drive, MaxAgeDays: maxAgeDays, MaxVersions: maxVersions})
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedHistorySettings(ctx, mysqlsql.InsertManagedHistorySettingsParams{Drive: []byte(drive), MaxAgeDays: maxAgeDays, MaxVersions: maxVersions})
	default:
		return dbsql.New(q.db).InsertManagedHistorySettings(ctx, dbsql.InsertManagedHistorySettingsParams{Drive: drive, MaxAgeDays: maxAgeDays, MaxVersions: maxVersions})
	}
}

func (q ManagedQueries) DeleteManagedHistorySettings(ctx context.Context, drive string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).DeleteManagedHistorySettings(ctx, drive)
	case MySQL:
		return mysqlsql.New(q.db).DeleteManagedHistorySettings(ctx, []byte(drive))
	default:
		return dbsql.New(q.db).DeleteManagedHistorySettings(ctx, drive)
	}
}

func (q ManagedQueries) AllManagedVersions(ctx context.Context) ([]ManagedVersion, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).AllManagedVersions(ctx)
		return convertManagedRows(rows, err, func(v pgsql.ManagedVersion) ManagedVersion { return ManagedVersion(v) })
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedVersions(ctx)
		return convertManagedRows(rows, err, mysqlVersion)
	default:
		return dbsql.New(q.db).AllManagedVersions(ctx)
	}
}

func (q ManagedQueries) ManagedVersionsByPath(ctx context.Context, drive string, path string) ([]ManagedVersion, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).ManagedVersionsByPath(ctx, pgsql.ManagedVersionsByPathParams{Drive: drive, Path: path})
		return convertManagedRows(rows, err, func(v pgsql.ManagedVersion) ManagedVersion { return ManagedVersion(v) })
	case MySQL:
		rows, err := mysqlsql.New(q.db).ManagedVersionsByPath(ctx, mysqlsql.ManagedVersionsByPathParams{Drive: []byte(drive), Path: []byte(path)})
		return convertManagedRows(rows, err, mysqlVersion)
	default:
		return dbsql.New(q.db).ManagedVersionsByPath(ctx, dbsql.ManagedVersionsByPathParams{Drive: drive, Path: path})
	}
}

func (q ManagedQueries) ManagedVersionsByDrive(ctx context.Context, drive string) ([]ManagedVersion, error) {
	switch q.dialect {
	case PostgreSQL:
		rows, err := pgsql.New(q.db).ManagedVersionsByDrive(ctx, drive)
		return convertManagedRows(rows, err, func(v pgsql.ManagedVersion) ManagedVersion { return ManagedVersion(v) })
	case MySQL:
		rows, err := mysqlsql.New(q.db).ManagedVersionsByDrive(ctx, []byte(drive))
		return convertManagedRows(rows, err, mysqlVersion)
	default:
		return dbsql.New(q.db).ManagedVersionsByDrive(ctx, drive)
	}
}

func (q ManagedQueries) InsertManagedVersion(ctx context.Context, drive string, path string, revision string, size int64, modified int64, retired int64) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedVersion(ctx, pgsql.InsertManagedVersionParams{Drive: drive, Path: path, Revision: revision, Size: size, Modified: modified, Retired: retired})
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedVersion(ctx, mysqlsql.InsertManagedVersionParams{Drive: []byte(drive), Path: []byte(path), Revision: []byte(revision), Size: size, Modified: modified, Retired: retired})
	default:
		return dbsql.New(q.db).InsertManagedVersion(ctx, dbsql.InsertManagedVersionParams{Drive: drive, Path: path, Revision: revision, Size: size, Modified: modified, Retired: retired})
	}
}

func (q ManagedQueries) DeleteManagedVersion(ctx context.Context, revision string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).DeleteManagedVersion(ctx, revision)
	case MySQL:
		return mysqlsql.New(q.db).DeleteManagedVersion(ctx, []byte(revision))
	default:
		return dbsql.New(q.db).DeleteManagedVersion(ctx, revision)
	}
}

func (q ManagedQueries) AllManagedReclaims(ctx context.Context) ([]string, error) {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).AllManagedReclaims(ctx)
	case MySQL:
		rows, err := mysqlsql.New(q.db).AllManagedReclaims(ctx)
		return convertManagedRows(rows, err, func(v []byte) string { return string(v) })
	default:
		return dbsql.New(q.db).AllManagedReclaims(ctx)
	}
}

func (q ManagedQueries) InsertManagedReclaim(ctx context.Context, revision string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).InsertManagedReclaim(ctx, revision)
	case MySQL:
		return mysqlsql.New(q.db).InsertManagedReclaim(ctx, []byte(revision))
	default:
		return dbsql.New(q.db).InsertManagedReclaim(ctx, revision)
	}
}

func (q ManagedQueries) DeleteManagedReclaim(ctx context.Context, revision string) error {
	switch q.dialect {
	case PostgreSQL:
		return pgsql.New(q.db).DeleteManagedReclaim(ctx, revision)
	case MySQL:
		return mysqlsql.New(q.db).DeleteManagedReclaim(ctx, []byte(revision))
	default:
		return dbsql.New(q.db).DeleteManagedReclaim(ctx, revision)
	}
}
