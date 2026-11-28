package storage

import (
	"errors"
	"time"
)

type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

var ErrDirectoryNotEmpty = errors.New("directory not empty")
