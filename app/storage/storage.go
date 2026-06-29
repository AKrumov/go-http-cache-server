// Package storage provides cache storage backends for the Gradle remote build cache server.
package storage

import (
	"context"
	"io"
	"time"
)

// Backend defines the interface for cache storage implementations.
type Backend interface {
	// Head checks if an object exists and returns its size.
	Head(ctx context.Context, key string) (size int64, exists bool, err error)

	// Get retrieves an object. The caller must close the returned ReadCloser.
	Get(ctx context.Context, key string) (rc io.ReadCloser, size int64, modTime time.Time, exists bool, err error)

	// Put stores an object at the given key.
	Put(ctx context.Context, key string, r io.Reader, size int64) error

	// Delete removes the object at the given key. It is idempotent: deleting a
	// non-existent object returns no error.
	Delete(ctx context.Context, key string) error
}
