-- name: DeleteCatalogEntries :exec
DELETE FROM catalog_entries WHERE folder_hash = UNHEX(SHA2(sqlc.arg(folder), 256));
