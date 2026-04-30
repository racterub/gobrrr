// Package atomicfs provides atomic file writes via a sibling .tmp file plus
// rename. The parent directory is NOT fsync'd in this version; durability on
// power loss is added in Refactor #6b.
package atomicfs

import (
	"encoding/json"
	"os"
)

// WriteFile writes data to path atomically by creating path+".tmp" with the
// given permissions and renaming it over path. The parent directory must
// already exist.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
