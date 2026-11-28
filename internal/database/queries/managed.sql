-- name: ManagedAccountByEmail :one
SELECT * FROM managed_accounts WHERE email = sqlc.arg(email);

-- name: ManagedAccountBySession :one
SELECT a.* FROM managed_accounts a
JOIN managed_sessions s ON s.account_id = a.id
WHERE s.token_hash = sqlc.arg(token_hash) AND s.expires > sqlc.arg(expires);

-- name: InsertManagedAccount :exec
INSERT INTO managed_accounts (id, email, full_name, password_hash)
VALUES (sqlc.arg(id), sqlc.arg(email), sqlc.arg(full_name), sqlc.arg(password_hash));

-- name: InsertManagedSession :exec
INSERT INTO managed_sessions (token_hash, account_id, expires)
VALUES (sqlc.arg(token_hash), sqlc.arg(account_id), sqlc.arg(expires));

-- name: DeleteManagedSession :exec
DELETE FROM managed_sessions WHERE token_hash = sqlc.arg(token_hash);

-- name: DeleteExpiredManagedSessions :exec
DELETE FROM managed_sessions WHERE expires <= sqlc.arg(expires);

-- name: ManagedDriveByID :one
SELECT id FROM managed_drives WHERE id = sqlc.arg(id);

-- name: InsertManagedDrive :exec
INSERT INTO managed_drives (id) VALUES (sqlc.arg(id));

-- name: ManagedDriveOwner :one
SELECT account_id FROM managed_owners WHERE drive = sqlc.arg(drive);

-- name: InsertManagedOwner :exec
INSERT INTO managed_owners (drive, account_id) VALUES (sqlc.arg(drive), sqlc.arg(account_id));

-- name: ManagedGrantByAccount :one
SELECT * FROM managed_grants WHERE drive = sqlc.arg(drive) AND account_id = sqlc.arg(account_id);

-- name: InsertManagedGrant :exec
INSERT INTO managed_grants (drive, account_id, pending, writable)
VALUES (sqlc.arg(drive), sqlc.arg(account_id), sqlc.arg(pending), sqlc.arg(writable));

-- name: UpdateManagedGrantAccess :exec
UPDATE managed_grants SET writable = sqlc.arg(writable)
WHERE drive = sqlc.arg(drive) AND account_id = sqlc.arg(account_id);

-- name: DeleteManagedGrant :exec
DELETE FROM managed_grants WHERE drive = sqlc.arg(drive) AND account_id = sqlc.arg(account_id);

-- name: AcceptManagedGrant :execrows
UPDATE managed_grants SET pending = 0
WHERE drive = sqlc.arg(drive) AND account_id = sqlc.arg(account_id) AND pending = 1;

-- name: ManagedOwnersByAccount :many
SELECT * FROM managed_owners WHERE account_id = sqlc.arg(account_id);

-- name: ManagedGrantsByAccount :many
SELECT * FROM managed_grants WHERE account_id = sqlc.arg(account_id);

-- name: ManagedDriveMembers :many
SELECT a.id, a.email, a.full_name, g.pending, g.writable
FROM managed_grants g JOIN managed_accounts a ON a.id = g.account_id
WHERE g.drive = sqlc.arg(drive) ORDER BY a.email;

-- name: ManagedFileByPath :one
SELECT * FROM managed_files WHERE drive = sqlc.arg(drive) AND path = sqlc.arg(path);

-- name: InsertManagedFile :exec
INSERT INTO managed_files (drive, path, revision, size, modified)
VALUES (sqlc.arg(drive), sqlc.arg(path), sqlc.arg(revision), sqlc.arg(size), sqlc.arg(modified));

-- name: UpdateManagedFile :exec
UPDATE managed_files SET revision = sqlc.arg(revision), size = sqlc.arg(size), modified = sqlc.arg(modified)
WHERE drive = sqlc.arg(drive) AND path = sqlc.arg(path);

-- name: DeleteManagedFile :exec
DELETE FROM managed_files WHERE drive = sqlc.arg(drive) AND path = sqlc.arg(path);

-- name: ManagedCommitByID :one
SELECT * FROM managed_commits WHERE drive = sqlc.arg(drive) AND member = sqlc.arg(member) AND id = sqlc.arg(id);

-- name: InsertManagedCommit :exec
INSERT INTO managed_commits (drive, member, id, path, base)
VALUES (sqlc.arg(drive), sqlc.arg(member), sqlc.arg(id), sqlc.arg(path), sqlc.arg(base));

-- name: InsertManagedLegacyMember :exec
INSERT INTO managed_members (drive, name, credential, invited, writable)
VALUES (sqlc.arg(drive), sqlc.arg(name), sqlc.arg(credential), sqlc.arg(invited), sqlc.arg(writable));

-- name: AllManagedDrives :many
SELECT id FROM managed_drives;

-- name: AllManagedAccounts :many
SELECT * FROM managed_accounts;

-- name: AllManagedOwners :many
SELECT * FROM managed_owners;

-- name: AllManagedGrants :many
SELECT * FROM managed_grants;

-- name: AllManagedFiles :many
SELECT * FROM managed_files;

-- name: AllManagedCommits :many
SELECT * FROM managed_commits;

-- name: AllManagedLegacyMembers :many
SELECT * FROM managed_members;

-- name: AnyManagedDrive :one
SELECT id FROM managed_drives LIMIT 1;

-- name: AnyManagedAccount :one
SELECT id FROM managed_accounts LIMIT 1;
