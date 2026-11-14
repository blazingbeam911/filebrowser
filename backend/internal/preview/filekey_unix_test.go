//go:build !windows

package preview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mustFileKey(t *testing.T, path string) (string, uint64) {
	t.Helper()
	hash, nlink, err := FileKey(path)
	if err != nil {
		t.Fatalf("FileKey(%s): %v", path, err)
	}
	if len(hash) != 32 {
		t.Fatalf("FileKey(%s) = %q, want 32 hex characters", path, hash)
	}
	return hash, nlink
}

func TestFileKeySameFileSameKey(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(a, []byte("video bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	keyA, nlink := mustFileKey(t, a)
	if nlink != 1 {
		t.Fatalf("nlink = %d, want 1", nlink)
	}

	// A hard link in another folder is the same file.
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(dir, "sub", "b.mp4")
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	keyB, nlinkB := mustFileKey(t, b)
	if keyB != keyA {
		t.Fatalf("hard link key %s, want %s", keyB, keyA)
	}
	if nlinkB != 2 {
		t.Fatalf("nlink after linking = %d, want 2", nlinkB)
	}

	// A symlink resolves to its target.
	s := filepath.Join(dir, "s.mp4")
	if err := os.Symlink(a, s); err != nil {
		t.Fatal(err)
	}
	if keyS, _ := mustFileKey(t, s); keyS != keyA {
		t.Fatalf("symlink key %s, want %s", keyS, keyA)
	}

	// Renamed (moved within the file system): same key.
	c := filepath.Join(dir, "sub", "renamed.mp4")
	if err := os.Rename(a, c); err != nil {
		t.Fatal(err)
	}
	if keyC, _ := mustFileKey(t, c); keyC != keyA {
		t.Fatalf("renamed key %s, want %s", keyC, keyA)
	}
}

func TestFileKeyDifferentFileOrContent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(a, []byte("video bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	if err := os.Chtimes(a, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	keyA, _ := mustFileKey(t, a)

	// A copy with the same bytes and mtime is another file (another inode).
	cp := filepath.Join(dir, "copy.mp4")
	if err := os.WriteFile(cp, []byte("video bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(cp, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if keyCp, _ := mustFileKey(t, cp); keyCp == keyA {
		t.Fatal("a copy got the original's key")
	}

	// A new mtime (to the nanosecond) changes the key.
	if err := os.Chtimes(a, mtime, mtime.Add(time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	keyT, _ := mustFileKey(t, a)
	if keyT == keyA {
		t.Fatal("key unchanged after the mtime changed")
	}

	// A new size with the mtime put back changes the key.
	if err := os.WriteFile(a, []byte("video bytes, longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(a, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if keyS, _ := mustFileKey(t, a); keyS == keyA || keyS == keyT {
		t.Fatal("key unchanged after the size changed")
	}
}

func TestFileKeyFromInfoMatchesFileKey(t *testing.T) {
	a := filepath.Join(t.TempDir(), "a.jpg")
	if err := os.WriteFile(a, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(a)
	if err != nil {
		t.Fatal(err)
	}
	fromInfo, _, err := FileKeyFromInfo(a, info)
	if err != nil {
		t.Fatal(err)
	}
	if direct, _ := mustFileKey(t, a); direct != fromInfo {
		t.Fatalf("FileKeyFromInfo %s, FileKey %s", fromInfo, direct)
	}
}

func TestFileKeyMissingFile(t *testing.T) {
	if _, _, err := FileKey(filepath.Join(t.TempDir(), "gone.mp4")); err == nil {
		t.Fatal("FileKey of a missing file returned no error")
	}
}

// fsidOf must give the value `stat -f -c %i` prints, so the logged IDs can be compared with it.
func TestFsidOfMatchesStatCommand(t *testing.T) {
	dir := t.TempDir()
	out, err := exec.Command("stat", "-f", "-c", "%i", dir).Output()
	if err != nil {
		t.Skipf("GNU stat not available: %v", err)
	}
	want, err := strconv.ParseUint(strings.TrimSpace(string(out)), 16, 64)
	if err != nil {
		t.Skipf("unexpected stat output %q", out)
	}
	got, err := fsidOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("fsidOf = %x, stat -f says %x", got, want)
	}
}
