-- +goose Up
CREATE TABLE write_queue_jobs (
    queue_id VARCHAR(64) NOT NULL,
    job_id VARCHAR(64) NOT NULL,
    payload TEXT NOT NULL,
    PRIMARY KEY (queue_id, job_id)
);

-- +goose Down
DROP TABLE write_queue_jobs;
