-- name: ManagedFilesByPrefix :many
SELECT * FROM managed_files
WHERE drive = sqlc.arg(drive)
  AND substr(path, 1, length(CAST(sqlc.arg(prefix_length_key) AS TEXT))) = CAST(sqlc.arg(prefix) AS TEXT)
ORDER BY path;

-- name: ManagedPathCollision :one
SELECT path FROM managed_files
WHERE drive = sqlc.arg(drive)
  AND (instr(path, CAST(sqlc.arg(descendant_prefix) AS TEXT)) = 1 OR
       instr(CAST(sqlc.arg(path) AS TEXT), concat(path, '/')) = 1)
LIMIT 1;
