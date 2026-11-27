-- name: SaveWriteJob :exec
INSERT INTO write_queue_jobs (queue_id, job_id, payload)
VALUES (sqlc.arg(queue_id), sqlc.arg(job_id), sqlc.arg(payload))
ON DUPLICATE KEY UPDATE payload = sqlc.arg(payload);
