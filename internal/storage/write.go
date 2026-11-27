package storage

import "errors"

// The commit marker lives on the current object. It identifies a completed
// upload after an interrupted response without keeping extra remote copies.
const commitMetadata = "freesync_commit"

var ErrUploadTooLarge = errors.New("file exceeds the provider upload limit")
