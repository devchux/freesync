-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS managed_accounts (
    id VARBINARY(64) PRIMARY KEY,
    email VARBINARY(254) NOT NULL UNIQUE,
    full_name VARBINARY(255) NOT NULL,
    password_hash VARBINARY(60) NOT NULL
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS managed_sessions (
    token_hash VARBINARY(64) PRIMARY KEY,
    account_id VARBINARY(64) NOT NULL,
    FOREIGN KEY (account_id) REFERENCES managed_accounts(id),
    expires BIGINT NOT NULL
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS managed_owners (
    drive VARBINARY(255) PRIMARY KEY,
    FOREIGN KEY (drive) REFERENCES managed_drives(id),
    account_id VARBINARY(64) NOT NULL,
    FOREIGN KEY (account_id) REFERENCES managed_accounts(id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS managed_grants (
    drive VARBINARY(255) NOT NULL,
    FOREIGN KEY (drive) REFERENCES managed_drives(id),
    account_id VARBINARY(64) NOT NULL,
    FOREIGN KEY (account_id) REFERENCES managed_accounts(id),
    pending INTEGER NOT NULL,
    writable INTEGER NOT NULL,
    PRIMARY KEY (drive, account_id)
) ENGINE=InnoDB;
-- +goose Down
DROP TABLE IF EXISTS managed_grants;
DROP TABLE IF EXISTS managed_owners;
DROP TABLE IF EXISTS managed_sessions;
DROP TABLE IF EXISTS managed_accounts;
