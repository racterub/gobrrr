// Package atomicfs provides atomic file writes via a sibling .tmp file plus
// rename, with a parent-directory fsync after the rename so the directory
// entry survives power loss. The parent directory must already exist; mkdir
// is the caller's job.
package atomicfs

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// fsyncDir opens dir and calls Sync on it so the most recent rename within
// dir is durable on the filesystem. It is a package variable so tests can
// substitute a stub via export_test.go.
var fsyncDir = func(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// WriteFile writes data to path atomically by creating path+".tmp" with the
// given permissions, renaming it over path, then fsync'ing the parent
// directory. The parent directory must already exist.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return fsyncDir(filepath.Dir(path))
}

// WriteJSON marshals v with two-space indentation and writes the result
// atomically via WriteFile. Callers needing a non-default JSON format
// (compact, four-space, etc.) should marshal themselves and call WriteFile.
func WriteJSON(path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, data, perm)
}
