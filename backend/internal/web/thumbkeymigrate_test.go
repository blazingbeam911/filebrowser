//go:build !windows

package web

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/internal/preview"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
)

// oldThumbHash is the pre-file-key cache hash, written out independently of the code under
// test: md5 of "<real path>:<size rounded up to 4 KiB>:<mtime RFC3339Nano>".
func oldThumbHash(t *testing.T, realPath string) string {
	t.Helper()
	fi, err := os.Stat(realPath)
	if err != nil {
		t.Fatal(err)
	}
	size := fi.Size()
	if size > 0 {
		size = (size + 4095) / 4096 * 4096
	}
	sum := md5.Sum([]byte(fmt.Sprintf("%s:%d:%s", realPath, size, fi.ModTime().Format(time.RFC3339Nano))))
	return hex.EncodeToString(sum[:])
}

func newThumbHash(t *testing.T, realPath string) string {
	t.Helper()
	h, _, err := preview.FileKey(realPath)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// thumbCacheEntries counts which of hash's entries (both sizes, at pcts) are in the cache.
func thumbCacheEntries(dir, hash string, pcts ...int) int {
	n := 0
	for _, pct := range pcts {
		for _, size := range []string{"small", "large"} {
			if _, err := os.Stat(thumbCacheFile(dir, preview.CacheKey(hash, size, pct))); err == nil {
				n++
			}
		}
	}
	return n
}

// thumbCacheFile is diskcache's file name for a key: <dir>/<h[0]>/<h[1:3]>/<h>, h = sha1(key).
func thumbCacheFile(dir, key string) string {
	h := sha1Hex(key)
	return filepath.Join(dir, h[:1], h[1:3], h)
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func putThumbCacheEntries(t *testing.T, dir, hash string, pcts ...int) {
	t.Helper()
	for _, pct := range pcts {
		for _, size := range []string{"small", "large"} {
			p := thumbCacheFile(dir, preview.CacheKey(hash, size, pct))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("thumbnail bytes"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func insertTypedRows(t *testing.T, source string, rows map[string]string) {
	t.Helper()
	db := indexing.GetIndexDB()
	for path, typ := range rows {
		info := &iteminfo.FileInfo{Path: path, ItemInfo: iteminfo.ItemInfo{Name: filepath.Base(path), ModTime: time.Unix(1, 0), Type: typ, HasPreview: true}}
		if err := db.InsertItem(source, path, info); err != nil {
			t.Fatal(err)
		}
	}
}

func setThumbKeyTestSizes(t *testing.T) {
	prevPage, prevWorkers, prevWait := thumbKeyPageSize, thumbKeyWorkers, thumbKeyReadyWait
	thumbKeyPageSize, thumbKeyWorkers, thumbKeyReadyWait = 2, 2, 10*time.Millisecond
	t.Cleanup(func() { thumbKeyPageSize, thumbKeyWorkers, thumbKeyReadyWait = prevPage, prevWorkers, prevWait })
}

func TestThumbKeyMigration(t *testing.T) {
	startThumbJobPreviews(t)
	root, name, _, _ := setupThumbJobSource(t)
	setThumbKeyTestSizes(t)
	dir := preview.CacheDir()
	if dir == "" {
		t.Fatal("no cache dir")
	}
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(dir, preview.RekeyMarkerName))
		_ = os.Remove(filepath.Join(dir, thumbKeyCursorName))
	})

	write := func(rel string) string {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("content of "+rel), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	img := write("pics/a.png")
	vid := write("post/v.mp4")
	link := filepath.Join(root, "links", "v.mp4")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(vid, link); err != nil {
		t.Fatal(err)
	}
	sym := filepath.Join(root, "pics", "sym.png")
	if err := os.Symlink(img, sym); err != nil {
		t.Fatal(err)
	}
	insertTypedRows(t, name, map[string]string{
		"/pics/a.png":    "image/png",
		"/pics/gone.png": "image/png",
		"/pics/sym.png":  "image/png",
		"/post/v.mp4":    "video/mp4",
		"/links/v.mp4":   "video/mp4",
	})

	imgOld := oldThumbHash(t, img)
	vidOld := oldThumbHash(t, vid)
	linkOld := oldThumbHash(t, link)
	putThumbCacheEntries(t, dir, imgOld, 0)
	putThumbCacheEntries(t, dir, vidOld, 0, 50, 100)
	putThumbCacheEntries(t, dir, linkOld, 0, 25)

	if err := runThumbKeyMigration(dir); err != nil {
		t.Fatal(err)
	}

	imgNew, vidNew := newThumbHash(t, img), newThumbHash(t, vid)
	if newThumbHash(t, link) != vidNew {
		t.Fatal("hard links got different keys")
	}
	if n := thumbCacheEntries(dir, imgNew, 0); n != 2 {
		t.Errorf("image: %d of 2 entries under the new key", n)
	}
	if n := thumbCacheEntries(dir, vidNew, 0, 25, 50, 100); n != 8 {
		t.Errorf("video: %d of 8 entries under the new key (0/50/100 from one link, 25 from the other)", n)
	}
	for what, h := range map[string]string{"image": imgOld, "video": vidOld, "link": linkOld} {
		if n := thumbCacheEntries(dir, h, 0, 25, 50, 100); n != 0 {
			t.Errorf("%s: %d entries left under the old key", what, n)
		}
	}
	marker, err := os.ReadFile(filepath.Join(dir, preview.RekeyMarkerName))
	if err != nil {
		t.Fatal(err)
	}
	// 5 files; moved: image 2 + one link's 6 + the other link's 2 at pct 25; the two links'
	// pct 0 entries collide (2 dropped); gone.png and the symlink are skipped.
	if want := "5 files, 10 entries moved, 2 duplicates deleted (30 bytes freed), 2 files skipped, 0 errors"; !strings.Contains(string(marker), want) {
		t.Errorf("marker %q, want it to say %q", marker, want)
	}
	if _, err := os.Stat(filepath.Join(dir, thumbKeyCursorName)); !os.IsNotExist(err) {
		t.Errorf("the saved place is still there: %v", err)
	}
}

// A saved place makes the walk carry on after it; files before it are not looked at again.
func TestThumbKeyMigrationResumes(t *testing.T) {
	startThumbJobPreviews(t)
	root, name, _, _ := setupThumbJobSource(t)
	setThumbKeyTestSizes(t)
	dir := preview.CacheDir()
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(dir, preview.RekeyMarkerName))
		_ = os.Remove(filepath.Join(dir, thumbKeyCursorName))
	})
	var paths []string
	for _, rel := range []string{"a/1.png", "a/2.png", "b/3.png"} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
		putThumbCacheEntries(t, dir, oldThumbHash(t, p), 0)
	}
	insertTypedRows(t, name, map[string]string{"/a/1.png": "image/png", "/a/2.png": "image/png", "/b/3.png": "image/png"})
	if err := saveThumbKeyCursor(filepath.Join(dir, thumbKeyCursorName), thumbKeyCursor{Source: name, After: "/a/2.png"}); err != nil {
		t.Fatal(err)
	}
	if err := runThumbKeyMigration(dir); err != nil {
		t.Fatal(err)
	}
	if n := thumbCacheEntries(dir, oldThumbHash(t, paths[0]), 0) + thumbCacheEntries(dir, oldThumbHash(t, paths[1]), 0); n != 4 {
		t.Errorf("files before the saved place: %d of 4 old entries left, want all", n)
	}
	if n := thumbCacheEntries(dir, newThumbHash(t, paths[2]), 0); n != 2 {
		t.Errorf("file after the saved place: %d of 2 entries moved", n)
	}
}

// An empty index is not taken as "nothing to do": no marker, so the next start tries again.
func TestThumbKeyMigrationEmptyIndex(t *testing.T) {
	startThumbJobPreviews(t)
	setupThumbJobSource(t)
	setThumbKeyTestSizes(t)
	dir := preview.CacheDir()
	if err := runThumbKeyMigration(dir); err == nil {
		t.Fatal("no error with an empty index")
	}
	if _, err := os.Stat(filepath.Join(dir, preview.RekeyMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("marker written: %v", err)
	}
}

// The delete handlers' helper finds the entry from its index path: one of two hard links keeps
// the thumbnails, the last link deletes them, a directory never does.
func TestThumbsBeforeDeleteFromIndexPath(t *testing.T) {
	startThumbJobPreviews(t)
	root, name, _, _ := setupThumbJobSource(t)
	dir := preview.CacheDir()
	idx := indexing.GetIndex(name)
	a := filepath.Join(root, "post", "a.mp4")
	if err := os.MkdirAll(filepath.Dir(a), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(root, "b.mp4")
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	hash := newThumbHash(t, a)
	putThumbCacheEntries(t, dir, hash, 0, 50)
	video := &iteminfo.ExtendedFileInfo{FileInfo: iteminfo.FileInfo{ItemInfo: iteminfo.ItemInfo{Type: "video/mp4"}}}

	thumbs := thumbsBeforeDelete(idx, "/b.mp4", video)
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	thumbs.Delete(t.Context())
	if n := thumbCacheEntries(dir, hash, 0, 50); n != 4 {
		t.Fatalf("after deleting one of two links: %d of 4 entries left", n)
	}

	folder := &iteminfo.ExtendedFileInfo{FileInfo: iteminfo.FileInfo{ItemInfo: iteminfo.ItemInfo{Type: "directory"}}}
	thumbsBeforeDelete(idx, "/post", folder).Delete(t.Context())

	thumbs = thumbsBeforeDelete(idx, "/post/a.mp4", video)
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	thumbs.Delete(t.Context())
	if n := thumbCacheEntries(dir, hash, 0, 50); n != 0 {
		t.Fatalf("after deleting the last link: %d entries left", n)
	}
}
