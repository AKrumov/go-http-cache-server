package storage

import (
	"context"
	"io"
	"time"

	"golang.org/x/sync/singleflight"
)

// SingleFlightBackend wraps a Backend and deduplicates in-flight requests
// for the same key. Only HEAD and PUT are deduplicated; GET is not because
// the returned ReadCloser is a single-use stream.
type SingleFlightBackend struct {
	backend Backend
	group   singleflight.Group
}

// NewSingleFlightBackend creates a new singleflight wrapper.
func NewSingleFlightBackend(b Backend) *SingleFlightBackend {
	return &SingleFlightBackend{backend: b}
}

// Head implements Backend.
func (s *SingleFlightBackend) Head(ctx context.Context, key string) (size int64, exists bool, err error) {
	v, err, _ := s.group.Do("head:"+key, func() (interface{}, error) {
		size, exists, err := s.backend.Head(ctx, key)
		if err != nil {
			return nil, err
		}
		return headResult{size: size, exists: exists}, nil
	})
	if err != nil {
		return 0, false, err
	}
	res := v.(headResult)
	return res.size, res.exists, nil
}

// Get implements Backend. Not deduplicated because ReadCloser is single-use.
func (s *SingleFlightBackend) Get(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
	return s.backend.Get(ctx, key)
}

// Put implements Backend.
func (s *SingleFlightBackend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err, _ := s.group.Do("put:"+key, func() (interface{}, error) {
		return nil, s.backend.Put(ctx, key, r, size)
	})
	return err
}

// Delete implements Backend.
func (s *SingleFlightBackend) Delete(ctx context.Context, key string) error {
	return s.backend.Delete(ctx, key)
}

type headResult struct {
	size   int64
	exists bool
}

var _ Backend = (*SingleFlightBackend)(nil)
