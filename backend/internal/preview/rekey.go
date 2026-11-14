package preview

// fufu: moving the thumbnail cache from the old path keys to file keys (filekey_unix.go), once,
// so thumbnails made before the change are found under the new keys.

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RekeyMarkerName is the file in the cache directory that says the cache has been re-keyed.
const RekeyMarkerName = ".key-v2"

// RekeyResult counts what RekeyFile did with one file's cache entries.
type RekeyResult struct {
	Moved   int   // entries moved to the new key
	Dropped int   // old entries deleted because the new key already had one
	Freed   int64 // bytes of the dropped entries
}

// renamer is a cache that can move an entry to another key (diskcache.FileCache).
type renamer interface {
	Rename(oldKey, newKey string) (moved, dropped bool, freed int64, err error)
}

// CacheDir returns the directory the thumbnail cache's entries are in ("" when the preview
// service isn't running or its cache can't re-key).
func CacheDir() string {
	if service == nil {
		return ""
	}
	if _, ok := service.fileCache.(renamer); !ok {
		return ""
	}
	return filepath.Join(service.cacheDir, "diskcache")
}

// oldPathHash is the cache hash previews had before file keys: the path, the size as the index
// shows it (oldSize: rounded up to whole blocks unless the source shows logical sizes) and the
// modification time.
func oldPathHash(realPath string, oldSize int64, modTime time.Time) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%s:%d:%s", realPath, oldSize, modTime.Format(time.RFC3339Nano))))
	return hex.EncodeToString(sum[:])
}

// RekeyFile moves the cached previews of the file at realPath (stat'ed as info, not a symlink)
// from its old path key to its file key: both sizes at seek 0 and, for a video, at every seek
// percentage. oldSize is the size the old key used (the index's display size).
func RekeyFile(realPath string, info os.FileInfo, oldSize int64, video bool) (RekeyResult, error) {
	var res RekeyResult
	if service == nil {
		return res, errNoService
	}
	rn, ok := service.fileCache.(renamer)
	if !ok {
		return res, fmt.Errorf("the cache can't re-key entries")
	}
	newHash, _, err := FileKeyFromInfo(realPath, info)
	if err != nil {
		return res, err
	}
	oldHash := oldPathHash(realPath, oldSize, info.ModTime())
	maxPct := 0
	if video {
		maxPct = 100
	}
	for pct := 0; pct <= maxPct; pct++ {
		for _, size := range []string{"small", "large"} {
			moved, dropped, freed, err := rn.Rename(CacheKey(oldHash, size, pct), CacheKey(newHash, size, pct))
			if err != nil {
				return res, err
			}
			if moved {
				res.Moved++
			}
			if dropped {
				res.Dropped++
				res.Freed += freed
			}
		}
	}
	return res, nil
}
