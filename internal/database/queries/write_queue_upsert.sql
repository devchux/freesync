-- name: SaveWriteJob :exec
INSERT INTO write_queue_jobs (queue_id, job_id, payload)
VALUES (sqlc.arg(queue_id), sqlc.arg(job_id), sqlc.arg(payload))
ON CONFLICT (queue_id, job_id) DO UPDATE SET payload = excluded.payload;
