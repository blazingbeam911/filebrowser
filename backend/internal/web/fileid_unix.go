//go:build !windows

package web

import (
	"os"
	"syscall"
)

// statFileID returns the identity of the file at path (device and inode, following
// symlinks) and its hard link count. Paths with the same identity are hard links to one
// file. ok is false when the identity can't be read.
func statFileID(path string) (id fileID, nlink uint64, ok bool) {
	info, err := os.Stat(path)
	if err != nil {
		return fileID{}, 0, false
	}
	st, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat {
		return fileID{}, 0, false
	}
	// The conversions are needed: the field types differ between platforms.
	return fileID{dev: uint64(st.Dev), ino: uint64(st.Ino)}, uint64(st.Nlink), true
}
