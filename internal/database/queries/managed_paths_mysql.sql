-- name: ManagedFilesByPrefix :many
SELECT * FROM managed_files
WHERE drive = sqlc.arg(drive)
  AND substr(path, 1, length(CAST(sqlc.arg(prefix_length_key) AS BINARY))) = CAST(sqlc.arg(prefix) AS BINARY)
ORDER BY path;

-- name: ManagedPathCollision :one
SELECT path FROM managed_files
WHERE drive = sqlc.arg(drive)
  AND (instr(path, CAST(sqlc.arg(descendant_prefix) AS BINARY)) = 1 OR
       instr(CAST(sqlc.arg(path) AS BINARY), concat(path, '/')) = 1)
LIMIT 1;
