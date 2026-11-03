//go:build linux

package indexing

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestGetCreatedTime_Symlink checks that the reported birth time follows the supplied
// FileInfo: the link's own time for Lstat info, the target's time for Stat info.
func TestGetCreatedTime_Symlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link")

	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ensure the link's birth time differs from the target's.
	time.Sleep(50 * time.Millisecond)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	followedInfo, err := os.Stat(link)
	if err != nil {
		t.Fatal(err)
	}

	targetCreated := getCreatedTime(targetInfo, target)
	linkCreated := getCreatedTime(linkInfo, link)
	if targetCreated == nil || linkCreated == nil {
		t.Skip("filesystem does not report birth time")
	}
	if !linkCreated.After(*targetCreated) {
		t.Skipf("filesystem birth times are too coarse to distinguish link (%v) from target (%v)", linkCreated, targetCreated)
	}

	if got := getCreatedTime(linkInfo, link); got == nil || !got.Equal(*linkCreated) {
		t.Errorf("Lstat info: got %v, want link birth time %v", got, linkCreated)
	}
	if got := getCreatedTime(followedInfo, link); got == nil || !got.Equal(*targetCreated) {
		t.Errorf("Stat info: got %v, want target birth time %v", got, targetCreated)
	}
}

// A version 5 user.DOSATTRIB value as written by Samba for a file copied in over SMB
// with its timestamps kept: attributes 0x20 (archive), creation time present.
const realDosAttribV5 = "00000500050000001100000020000000be79c6c05696dc01"

var realDosAttribV5Time = time.Unix(1770265225, 985887800) // 2026-02-05 04:20:25.9858878 UTC

func dosAttribV5(t *testing.T) []byte {
	t.Helper()
	b, err := hex.DecodeString(realDosAttribV5)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseDosAttribCreateTime(t *testing.T) {
	if got := parseDosAttribCreateTime(dosAttribV5(t)); got == nil || !got.Equal(realDosAttribV5Time) {
		t.Fatalf("v5 value: got %v, want %v", got, realDosAttribV5Time)
	}

	tests := []struct {
		name   string
		mutate func(b []byte) []byte
	}{
		{"nil", func([]byte) []byte { return nil }},
		{"too short", func(b []byte) []byte { return b[:23] }},
		{"version 4", func(b []byte) []byte { binary.LittleEndian.PutUint16(b[2:4], 4); return b }},
		{"switch value 4", func(b []byte) []byte { binary.LittleEndian.PutUint16(b[4:6], 4); return b }},
		{"non-empty attrib_hex", func(b []byte) []byte { b[0] = '0'; return b }},
		{"create-time flag unset", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[8:12], 0x01); return b }},
		{"zero create time", func(b []byte) []byte { binary.LittleEndian.PutUint64(b[16:24], 0); return b }},
		{"create time before 1970", func(b []byte) []byte { binary.LittleEndian.PutUint64(b[16:24], ntTimeUnixEpoch-1); return b }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDosAttribCreateTime(tt.mutate(dosAttribV5(t))); got != nil {
				t.Errorf("got %v, want nil", got)
			}
		})
	}
}

// setDosAttrib writes the v5 test value to path, skipping the test when the
// filesystem doesn't support user xattrs.
func setDosAttrib(t *testing.T, path string) {
	t.Helper()
	if err := unix.Setxattr(path, dosAttribXattr, dosAttribV5(t), 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
			t.Skip("filesystem does not support user xattrs")
		}
		t.Fatal(err)
	}
}

// TestGetCreatedTime_DosAttrib checks that a Samba creation time wins over the birth
// time for files and folders, through both a directory-listing statx result and a
// plain Lstat result, and that items without one keep their birth time.
func TestGetCreatedTime_DosAttrib(t *testing.T) {
	dir := t.TempDir()
	withFile := filepath.Join(dir, "with.txt")
	withDir := filepath.Join(dir, "withdir")
	plainFile := filepath.Join(dir, "plain.txt")
	for _, f := range []string{withFile, plainFile} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(withDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setDosAttrib(t, withFile)
	setDosAttrib(t, withDir)

	dirFd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dirFd)

	for _, name := range []string{"with.txt", "withdir"} {
		path := filepath.Join(dir, name)
		listed, err := statDirEntry(dirFd, name, true)
		if err != nil {
			t.Fatal(err)
		}
		lstat, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		for kind, info := range map[string]os.FileInfo{"listing": listed, "lstat": lstat} {
			if got := getCreatedTime(info, path); got == nil || !got.Equal(realDosAttribV5Time) {
				t.Errorf("%s via %s: got %v, want %v", name, kind, got, realDosAttribV5Time)
			}
		}
	}

	plainInfo, err := os.Lstat(plainFile)
	if err != nil {
		t.Fatal(err)
	}
	var stx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, plainFile, 0, unix.STATX_BTIME, &stx); err != nil || stx.Mask&unix.STATX_BTIME == 0 {
		if got := getCreatedTime(plainInfo, plainFile); got != nil {
			t.Errorf("plain file without birth time: got %v, want nil", got)
		}
		return
	}
	want := time.Unix(stx.Btime.Sec, int64(stx.Btime.Nsec))
	if got := getCreatedTime(plainInfo, plainFile); got == nil || !got.Equal(want) {
		t.Errorf("plain file: got %v, want birth time %v", got, want)
	}
}

// TestGetCreatedTime_DosAttribSymlink checks that the Samba creation time follows the
// supplied FileInfo like the birth time does: Stat info on a link reads the target's.
func TestGetCreatedTime_DosAttribSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	setDosAttrib(t, target)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	followed, err := os.Stat(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := getCreatedTime(followed, link); got == nil || !got.Equal(realDosAttribV5Time) {
		t.Errorf("Stat info: got %v, want target's Samba time %v", got, realDosAttribV5Time)
	}
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := getCreatedTime(linkInfo, link); got != nil && got.Equal(realDosAttribV5Time) {
		t.Errorf("Lstat info: got the target's Samba time %v, want the link's own birth time", got)
	}
}
