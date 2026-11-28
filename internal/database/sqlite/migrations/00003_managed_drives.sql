-- +goose Up
CREATE TABLE managed_drives (id TEXT PRIMARY KEY);
CREATE TABLE managed_members (
    drive TEXT NOT NULL REFERENCES managed_drives(id),
    name TEXT NOT NULL,
    credential TEXT NOT NULL UNIQUE,
    invited INTEGER NOT NULL,
    writable INTEGER NOT NULL,
    PRIMARY KEY (drive, name)
);
CREATE TABLE managed_files (
    drive TEXT NOT NULL REFERENCES managed_drives(id),
    path TEXT NOT NULL,
    revision TEXT NOT NULL UNIQUE,
    size INTEGER NOT NULL,
    modified INTEGER NOT NULL,
    PRIMARY KEY (drive, path)
);
CREATE TABLE managed_commits (
    drive TEXT NOT NULL REFERENCES managed_drives(id),
    member TEXT NOT NULL,
    id TEXT NOT NULL,
    path TEXT NOT NULL,
    base TEXT NOT NULL,
    PRIMARY KEY (drive, member, id)
);
-- +goose Down
DROP TABLE managed_commits;
DROP TABLE managed_files;
DROP TABLE managed_members;
DROP TABLE managed_drives;
