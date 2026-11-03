//go:build linux

package indexing

import (
	"encoding/binary"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// getCreatedTime returns the file's creation time when available. A creation time
// recorded by Samba (user.DOSATTRIB) wins, because Linux can't set a birth time:
// for files copied in over SMB with their timestamps kept, the birth time is only
// when the file arrived at this path. Otherwise it returns the birth time: directory
// listings embed it in statxFileInfo; other paths use statx on realPath (or nil).
func getCreatedTime(info os.FileInfo, realPath string) *time.Time {
	if info == nil {
		return nil
	}
	if realPath != "" {
		if t := dosAttribCreateTime(realPath, info.Mode()&os.ModeSymlink != 0); t != nil {
			return t
		}
	}
	if sxi, ok := info.(*statxFileInfo); ok {
		return sxi.birthTime()
	}
	if _, ok := info.(*fstatatFileInfo); ok {
		return nil
	}
	if realPath == "" {
		return nil
	}
	flags := 0
	if info.Mode()&os.ModeSymlink != 0 {
		flags = unix.AT_SYMLINK_NOFOLLOW
	}
	var stx unix.Statx_t
	err := unix.Statx(unix.AT_FDCWD, realPath, flags, unix.STATX_BTIME, &stx)
	if err != nil || stx.Mask&unix.STATX_BTIME == 0 || stx.Btime.Sec <= 0 {
		return nil
	}
	t := time.Unix(stx.Btime.Sec, int64(stx.Btime.Nsec))
	return &t
}

const dosAttribXattr = "user.DOSATTRIB"

// dosAttribCreateTime reads Samba's user.DOSATTRIB xattr from realPath and returns
// the creation time it records, or nil. noFollow reads a symlink's own xattrs,
// matching the birth-time lookup for the same FileInfo.
func dosAttribCreateTime(realPath string, noFollow bool) *time.Time {
	buf := make([]byte, 64)
	var n int
	var err error
	if noFollow {
		n, err = unix.Lgetxattr(realPath, dosAttribXattr, buf)
	} else {
		n, err = unix.Getxattr(realPath, dosAttribXattr, buf)
	}
	if err != nil {
		return nil
	}
	return parseDosAttribCreateTime(buf[:n])
}

// Samba's DOSATTRIB value, version 5 (librpc/idl/xattr.idl, xattr_DOSATTRIB with
// xattr_DosInfo5, marshalled by ndr_push_xattr_DOSATTRIB in librpc/ndr/ndr_xattr.c):
//
//	0-2   attrib_hex: empty NUL-terminated string (always empty for v5), padded to 2
//	2-4   version (uint16) = 5
//	4-8   union switch value (uint16) = 5, padded to 4
//	8-12  valid_flags (uint32)
//	12-16 attrib (uint32)
//	16-24 create_time (NTTIME: 100 ns units since 1601-01-01 UTC)
const (
	dosInfoV5Len          = 24
	dosInfoFlagCreateTime = 0x00000010 // XATTR_DOSINFO_CREATE_TIME
	ntTimeUnixEpoch       = 116444736000000000
	ntTimeTicksPerSecond  = 10000000
	ntTimeTickNanoseconds = 100
)

// parseDosAttribCreateTime returns the creation time in a version 5 DOSATTRIB value
// whose create-time flag is set, or nil for anything else (other versions included).
func parseDosAttribCreateTime(b []byte) *time.Time {
	if len(b) < dosInfoV5Len || b[0] != 0 {
		return nil
	}
	if binary.LittleEndian.Uint16(b[2:4]) != 5 || binary.LittleEndian.Uint16(b[4:6]) != 5 {
		return nil
	}
	if binary.LittleEndian.Uint32(b[8:12])&dosInfoFlagCreateTime == 0 {
		return nil
	}
	nt := binary.LittleEndian.Uint64(b[16:24])
	if nt <= ntTimeUnixEpoch {
		return nil
	}
	ticks := nt - ntTimeUnixEpoch
	t := time.Unix(int64(ticks/ntTimeTicksPerSecond), int64(ticks%ntTimeTicksPerSecond)*ntTimeTickNanoseconds)
	return &t
}
