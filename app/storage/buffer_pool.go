package storage

import (
	"io"
	"sync"
)

// bufferPool provides reusable byte buffers for streaming to reduce GC pressure.
// The pool returns 32 KiB buffers which is a good balance for S3 part sizes
// and network I/O.
var bufferPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024) // 32 KiB
		return &b
	},
}

// getBuffer returns a byte slice from the pool.
func getBuffer() *[]byte {
	return bufferPool.Get().(*[]byte)
}

// putBuffer returns a byte slice to the pool.
func putBuffer(b *[]byte) {
	bufferPool.Put(b)
}

// PooledCopy copies from src to dst using a pooled buffer.
func PooledCopy(dst io.Writer, src io.Reader) (int64, error) {
	buf := getBuffer()
	defer putBuffer(buf)
	return io.CopyBuffer(dst, src, *buf)
}
