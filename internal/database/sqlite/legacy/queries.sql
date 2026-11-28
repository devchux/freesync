-- name: HasLegacyWriteJobsTable :one
SELECT EXISTS (SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'writes');

-- name: ListLegacyWriteJobs :many
SELECT id, job FROM writes;
