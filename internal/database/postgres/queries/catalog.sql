-- name: DeleteCatalogEntries :exec
DELETE FROM catalog_entries WHERE folder = $1;

-- name: InsertCatalogEntry :exec
INSERT INTO catalog_entries (folder, path, directory, size)
VALUES ($1, $2, $3, $4);
