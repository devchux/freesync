-- name: DeleteCatalogEntries :exec
DELETE FROM catalog_entries WHERE folder_hash = UNHEX(SHA2(sqlc.arg(folder), 256));

-- name: InsertCatalogEntry :exec
INSERT INTO catalog_entries (folder, path, directory, size)
VALUES (?, ?, ?, ?);
