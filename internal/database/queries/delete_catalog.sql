-- name: DeleteCatalogEntries :exec
DELETE FROM catalog_entries WHERE folder = sqlc.arg(folder);
