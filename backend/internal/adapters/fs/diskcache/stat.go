package diskcache

// fufu: a size-only look at a cache entry, for the thumbnail job's quick check (it needs to
// know whether an entry is there and isn't the empty marker, without reading it).

import (
	"errors"
	"os"
)

// StatSize returns the stored size of key's value, and whether it exists.
func (f *FileCache) StatSize(key string) (int64, bool, error) {
	fi, err := os.Stat(f.getFileName(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return fi.Size(), true, nil
}
