-- +goose Up
CREATE TABLE managed_accounts (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    full_name TEXT NOT NULL,
    password_hash TEXT NOT NULL
);
CREATE TABLE managed_sessions (
    token_hash TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES managed_accounts(id),
    expires BIGINT NOT NULL
);
CREATE TABLE managed_owners (
    drive TEXT PRIMARY KEY REFERENCES managed_drives(id),
    account_id TEXT NOT NULL REFERENCES managed_accounts(id)
);
CREATE TABLE managed_grants (
    drive TEXT NOT NULL REFERENCES managed_drives(id),
    account_id TEXT NOT NULL REFERENCES managed_accounts(id),
    pending INTEGER NOT NULL,
    writable INTEGER NOT NULL,
    PRIMARY KEY (drive, account_id)
);
-- +goose Down
DROP TABLE managed_grants;
DROP TABLE managed_owners;
DROP TABLE managed_sessions;
DROP TABLE managed_accounts;
