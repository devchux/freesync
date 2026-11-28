-- +goose Up
CREATE TABLE managed_history_settings (
    drive VARBINARY(255) PRIMARY KEY,
    max_age_days BIGINT NOT NULL,
    max_versions BIGINT NOT NULL,
    FOREIGN KEY (drive) REFERENCES managed_drives(id)
) ENGINE=InnoDB;
CREATE TABLE managed_versions (
    drive VARBINARY(255) NOT NULL,
    path VARBINARY(2048) NOT NULL,
    revision VARBINARY(64) PRIMARY KEY,
    size BIGINT NOT NULL,
    modified BIGINT NOT NULL,
    retired BIGINT NOT NULL,
    FOREIGN KEY (drive) REFERENCES managed_drives(id)
) ENGINE=InnoDB;
CREATE INDEX managed_versions_path ON managed_versions (drive, path, retired, revision);
CREATE TABLE managed_reclaims (
    revision VARBINARY(64) PRIMARY KEY
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE managed_reclaims;
DROP TABLE managed_versions;
DROP TABLE managed_history_settings;
