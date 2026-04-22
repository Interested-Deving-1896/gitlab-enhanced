package storage

import "errors"

var (
	ErrNoBackendAvailable = errors.New("no storage backend available")
	ErrNotFound           = errors.New("object not found")
	ErrKeyInvalid         = errors.New("invalid object key")
)
