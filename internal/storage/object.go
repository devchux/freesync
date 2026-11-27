package storage

import "time"

type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}
