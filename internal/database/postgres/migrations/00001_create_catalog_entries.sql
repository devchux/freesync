-- +goose Up
CREATE TABLE catalog_entries (
    folder TEXT NOT NULL,
    path TEXT NOT NULL,
    directory BOOLEAN NOT NULL,
    size BIGINT NOT NULL,
    PRIMARY KEY (folder, path)
);

-- +goose Down
DROP TABLE catalog_entries;
