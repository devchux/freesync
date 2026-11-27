-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS write_queue_jobs (
    queue_id VARCHAR(64) NOT NULL,
    job_id VARCHAR(64) NOT NULL,
    payload BLOB NOT NULL,
    PRIMARY KEY (queue_id, job_id)
);

-- +goose Down
DROP TABLE IF EXISTS write_queue_jobs;
