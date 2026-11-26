-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS catalog_entries (
    folder BLOB NOT NULL,
    path BLOB NOT NULL,
    directory BOOLEAN NOT NULL,
    size BIGINT NOT NULL,
    folder_hash BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(folder, 256))) STORED,
    path_hash BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(path, 256))) STORED,
    UNIQUE KEY catalog_entry (folder_hash, path_hash)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE IF EXISTS catalog_entries;
