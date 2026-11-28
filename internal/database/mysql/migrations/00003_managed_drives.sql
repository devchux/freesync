-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS managed_drives (id VARBINARY(255) PRIMARY KEY) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS managed_members (
    drive VARBINARY(255) NOT NULL,
    FOREIGN KEY (drive) REFERENCES managed_drives(id),
    name VARBINARY(255) NOT NULL,
    credential VARBINARY(64) NOT NULL UNIQUE,
    invited INTEGER NOT NULL,
    writable INTEGER NOT NULL,
    PRIMARY KEY (drive, name)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS managed_files (
    drive VARBINARY(255) NOT NULL,
    FOREIGN KEY (drive) REFERENCES managed_drives(id),
    path VARBINARY(2048) NOT NULL,
    revision VARBINARY(64) NOT NULL UNIQUE,
    size BIGINT NOT NULL,
    modified BIGINT NOT NULL,
    PRIMARY KEY (drive, path)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS managed_commits (
    drive VARBINARY(255) NOT NULL,
    FOREIGN KEY (drive) REFERENCES managed_drives(id),
    member VARBINARY(64) NOT NULL,
    id VARBINARY(128) NOT NULL,
    path VARBINARY(2048) NOT NULL,
    base VARBINARY(64) NOT NULL,
    PRIMARY KEY (drive, member, id)
) ENGINE=InnoDB;
-- +goose Down
DROP TABLE IF EXISTS managed_commits;
DROP TABLE IF EXISTS managed_files;
DROP TABLE IF EXISTS managed_members;
DROP TABLE IF EXISTS managed_drives;
