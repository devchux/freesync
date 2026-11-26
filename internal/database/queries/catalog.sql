-- name: InsertCatalogEntry :exec
INSERT INTO catalog_entries (folder, path, directory, size)
VALUES (sqlc.arg(folder), sqlc.arg(path), sqlc.arg(directory), sqlc.arg(size));
