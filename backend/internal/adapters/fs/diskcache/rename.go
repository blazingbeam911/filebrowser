package diskcache

// fufu: moving a cache entry from one key to another, for re-keying the thumbnail cache.

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/gtsteffaniak/filebrowser/backend/internal/adapters/fs/fileutils"
)

// Rename moves the value stored under oldKey to newKey (moved). With nothing under oldKey it
// does nothing. When newKey already has a value, that one is kept and oldKey's is deleted
// (dropped, with freed its size). It takes no key locks: the scoped locks are kept for
// every key ever locked, and re-keying a whole cache would fill memory with them. Callers use
// it only for keys nothing else reads or writes, and a value stored under newKey at the same
// moment is a preview of the same file either way.
func (f *FileCache) Rename(oldKey, newKey string) (moved, dropped bool, freed int64, err error) {
	oldName := f.getFileName(oldKey)
	oldInfo, err := os.Stat(oldName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, 0, nil
		}
		return false, false, 0, err
	}
	newName := f.getFileName(newKey)
	if _, err := os.Stat(newName); err == nil {
		if err := os.Remove(oldName); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false, false, 0, nil // gone meanwhile
			}
			return false, false, 0, err
		}
		return false, true, oldInfo.Size(), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, false, 0, err
	}
	if err := os.MkdirAll(filepath.Dir(newName), fileutils.PermDir); err != nil {
		return false, false, 0, err
	}
	if err := os.Rename(oldName, newName); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, 0, nil // gone meanwhile
		}
		return false, false, 0, err
	}
	return true, false, 0, nil
}
