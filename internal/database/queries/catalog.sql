-- name: DeleteCatalogEntries :exec
DELETE FROM catalog_entries WHERE folder = ?;

-- name: InsertCatalogEntry :exec
INSERT INTO catalog_entries (folder, path, directory, size)
VALUES (?, ?, ?, ?);
