//go:build windows

package storage

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// accessTime returns the last access time of the file at path.
func accessTime(path string) (time.Time, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return time.Time{}, err
	}

	var data windows.Win32FileAttributeData
	if err := windows.GetFileAttributesEx(pathp, windows.GetFileExInfoStandard, (*byte)(unsafe.Pointer(&data))); err != nil {
		return time.Time{}, err
	}

	return time.Unix(0, data.LastAccessTime.Nanoseconds()), nil
}
