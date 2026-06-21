//go:build !windows

package storage

import (
	"time"

	"golang.org/x/sys/unix"
)

// accessTime returns the last access time of the file at path.
func accessTime(path string) (time.Time, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, unix.TimespecToNsec(stat.Atim)), nil
}
