-- +goose Up
CREATE TABLE managed_history_settings (
    drive TEXT PRIMARY KEY,
    max_age_days BIGINT NOT NULL,
    max_versions BIGINT NOT NULL,
    FOREIGN KEY (drive) REFERENCES managed_drives(id)
);
CREATE TABLE managed_versions (
    drive TEXT NOT NULL,
    path TEXT NOT NULL,
    revision TEXT PRIMARY KEY,
    size BIGINT NOT NULL,
    modified BIGINT NOT NULL,
    retired BIGINT NOT NULL,
    FOREIGN KEY (drive) REFERENCES managed_drives(id)
);
CREATE INDEX managed_versions_path ON managed_versions (drive, path, retired, revision);
CREATE TABLE managed_reclaims (
    revision TEXT PRIMARY KEY
);

-- +goose Down
DROP TABLE managed_reclaims;
DROP TABLE managed_versions;
DROP TABLE managed_history_settings;
