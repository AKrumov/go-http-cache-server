package main

import (
	"fmt"
	"path"
	"strings"
)

func parseCachePath(urlPath string) (cacheID string, entryKey string, err error) {
	const prefix = "/cache/"
	if !strings.HasPrefix(urlPath, prefix) {
		return "", "", fmt.Errorf("invalid cache prefix")
	}

	trimmed := strings.TrimPrefix(urlPath, prefix)
	if trimmed == "" || strings.Contains(trimmed, "..") || strings.ContainsAny(trimmed, "\\") {
		return "", "", fmt.Errorf("invalid cache path")
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid cache path")
	}

	cacheID = parts[0]
	entryKey = path.Clean(strings.Join(parts[1:], "/"))
	return cacheID, entryKey, nil
}

func makeStorageKey(cacheID, entryKey string) string {
	return cacheID + "/" + entryKey
}
