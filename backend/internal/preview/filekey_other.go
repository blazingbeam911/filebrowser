//go:build windows

package preview

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

// fsidOf is not available here; the key falls back to the path.
func fsidOf(string) (uint64, error) {
	return 0, fmt.Errorf("file system ID not available on this platform")
}

// FileKey returns the cache hash for the file at realPath. Without inode numbers it is keyed
// by path, size and modification time, so hard links and moved files don't share thumbnails.
// The link count is reported as 1.
func FileKey(realPath string) (hash string, nlink uint64, err error) {
	info, err := os.Stat(realPath)
	if err != nil {
		return "", 0, err
	}
	return FileKeyFromInfo(realPath, info)
}

// FileKeyFromInfo is FileKey for a file the caller has already stat'ed.
func FileKeyFromInfo(realPath string, info os.FileInfo) (hash string, nlink uint64, err error) {
	sum := md5.Sum([]byte(fmt.Sprintf("%s:%d:%s", realPath, info.Size(), info.ModTime().Format(time.RFC3339Nano))))
	return hex.EncodeToString(sum[:]), 1, nil
}

// logSourceFileSystems has nothing to log here: keys don't use file system IDs.
func logSourceFileSystems() {}
