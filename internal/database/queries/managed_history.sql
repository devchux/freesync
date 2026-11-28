-- name: AllManagedHistorySettings :many
SELECT * FROM managed_history_settings ORDER BY drive;

-- name: ManagedHistorySettingsByDrive :one
SELECT * FROM managed_history_settings WHERE drive = sqlc.arg(drive);

-- name: InsertManagedHistorySettings :exec
INSERT INTO managed_history_settings (drive, max_age_days, max_versions)
VALUES (sqlc.arg(drive), sqlc.arg(max_age_days), sqlc.arg(max_versions));

-- name: DeleteManagedHistorySettings :exec
DELETE FROM managed_history_settings WHERE drive = sqlc.arg(drive);

-- name: AllManagedVersions :many
SELECT * FROM managed_versions ORDER BY drive, path, retired DESC, revision DESC;

-- name: ManagedVersionsByPath :many
SELECT * FROM managed_versions WHERE drive = sqlc.arg(drive) AND path = sqlc.arg(path)
ORDER BY retired DESC, revision DESC;

-- name: InsertManagedVersion :exec
INSERT INTO managed_versions (drive, path, revision, size, modified, retired)
VALUES (sqlc.arg(drive), sqlc.arg(path), sqlc.arg(revision), sqlc.arg(size), sqlc.arg(modified), sqlc.arg(retired));

-- name: DeleteManagedVersion :exec
DELETE FROM managed_versions WHERE revision = sqlc.arg(revision);

-- name: AllManagedReclaims :many
SELECT revision FROM managed_reclaims ORDER BY revision;

-- name: InsertManagedReclaim :exec
INSERT INTO managed_reclaims (revision) VALUES (sqlc.arg(revision));

-- name: DeleteManagedReclaim :exec
DELETE FROM managed_reclaims WHERE revision = sqlc.arg(revision);

-- name: ManagedVersionsByDrive :many
SELECT * FROM managed_versions WHERE drive = sqlc.arg(drive)
ORDER BY path, retired DESC, revision DESC;
