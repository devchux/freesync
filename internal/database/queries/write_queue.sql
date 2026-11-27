-- name: ListWriteJobs :many
SELECT job_id, payload FROM write_queue_jobs WHERE queue_id = sqlc.arg(queue_id);

-- name: DeleteWriteJob :exec
DELETE FROM write_queue_jobs WHERE queue_id = sqlc.arg(queue_id) AND job_id = sqlc.arg(job_id);
