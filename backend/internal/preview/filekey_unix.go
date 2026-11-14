//go:build !windows

package preview

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"sync"
	"syscall"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/settings"
	"github.com/gtsteffaniak/go-logger/logger"
	"golang.org/x/sys/unix"
)

// fsidByDev caches each file system's f_fsid by the device number stat reports for it. Device
// numbers are handed out at mount time and can change across reboots, so they are only used
// as a lookup within this process; the key itself uses f_fsid, which file systems such as ZFS
// derive from an ID stored on disk.
var fsidByDev sync.Map // uint64 (st_dev) -> uint64 (f_fsid)

// fsidOf returns the f_fsid of the file system holding path, the 64-bit value `stat -f -c %i`
// prints: the first 32-bit word in the high half.
func fsidOf(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(uint32(st.Fsid.Val[0]))<<32 | uint64(uint32(st.Fsid.Val[1])), nil
}

// FileKey returns the cache hash for the file at realPath and its hard link count. The hash
// identifies the file, not the path: the file system (f_fsid), the inode, the size and the
// modification time. Every hard link to a file gets the same hash, and so does the file after
// it is renamed or moved within its file system; a change to the content (size or mtime)
// gives a new hash. Symlinks are followed.
func FileKey(realPath string) (hash string, nlink uint64, err error) {
	info, err := os.Stat(realPath)
	if err != nil {
		return "", 0, err
	}
	return FileKeyFromInfo(realPath, info)
}

// FileKeyFromInfo is FileKey for a file the caller has already stat'ed (info describes the
// file at realPath, not a symlink to it).
func FileKeyFromInfo(realPath string, info os.FileInfo) (hash string, nlink uint64, err error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", 0, fmt.Errorf("no file identity for %s", info.Name())
	}
	// The conversions are needed: the field types differ between platforms.
	dev := uint64(st.Dev)
	var fsid uint64
	if v, found := fsidByDev.Load(dev); found {
		fsid = v.(uint64)
	} else {
		fsid, err = fsidOf(realPath)
		if err != nil {
			return "", 0, err
		}
		fsidByDev.Store(dev, fsid)
	}
	sum := md5.Sum([]byte(fmt.Sprintf("v2:%016x:%d:%d:%d",
		fsid, uint64(st.Ino), info.Size(), info.ModTime().UnixNano())))
	return hex.EncodeToString(sum[:]), uint64(st.Nlink), nil
}

// logSourceFileSystems logs the file system ID of each source at start. Thumbnails are keyed by
// it, so if it ever changed (it shouldn't), every thumbnail would be made again; the log shows
// the value to compare.
func logSourceFileSystems() {
	names := make([]string, 0, len(settings.Config.Server.NameToSource))
	for name := range settings.Config.Server.NameToSource {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		src := settings.Config.Server.NameToSource[name]
		if fsid, err := fsidOf(src.Path); err != nil {
			logger.Warningf("Thumbnail keys: can't read the file system ID of source %q: %v", name, err)
		} else {
			logger.Infof("Thumbnail keys: source %q is on file system %x", name, fsid)
		}
	}
}
