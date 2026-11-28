CREATE TABLE writes (id TEXT PRIMARY KEY, job BLOB NOT NULL);

-- SQLite provides this table at runtime. Declare the queried columns for sqlc.
CREATE TABLE sqlite_schema (type TEXT NOT NULL, name TEXT NOT NULL);
