//go:build !windows

package preview

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The old key matches the formula proven against a real cache (a Python reimplementation that
// found the cached thumbnails of real files): md5 of "<path>:<display size>:<mtime RFC3339Nano>".
func TestOldPathHashFormula(t *testing.T) {
	cases := []struct {
		path string
		size int64
		mod  time.Time
		want string
	}{
		{"/data/media/a b/c [1].mp4", 8192, time.Unix(1700000000, 123456789).UTC(), "e1d44f06c382190b970b8819ea7f23fc"},
		{"/data/media/x.png", 4096, time.Unix(1700000000, 0).UTC(), "be12f5fdb8c4dfd6eca57d89834d7082"},
	}
	for _, c := range cases {
		if got := oldPathHash(c.path, c.size, c.mod); got != c.want {
			t.Errorf("oldPathHash(%q) = %s, want %s", c.path, got, c.want)
		}
	}
}

func TestRekeyFile(t *testing.T) {
	svc := NewPreviewGenerator(1, t.TempDir())
	useService(t, svc)
	dir := t.TempDir()

	// An image: both sizes at seek 0 move; a failed attempt's empty marker moves too.
	img := filepath.Join(dir, "a.jpg")
	writeFile(t, img)
	info, err := os.Stat(img)
	if err != nil {
		t.Fatal(err)
	}
	oldHash := oldPathHash(img, 4096, info.ModTime())
	storeThumbs(t, svc, oldHash, 0)
	if err := svc.fileCache.Store(t.Context(), CacheKey(oldHash, "large", 0), []byte{}); err != nil {
		t.Fatal(err)
	}
	res, err := RekeyFile(img, info, 4096, false)
	if err != nil {
		t.Fatal(err)
	}
	newHash, _ := mustFileKey(t, img)
	if res.Moved != 2 || res.Dropped != 0 || thumbsPresent(t, svc, newHash, 0) != 2 || thumbsPresent(t, svc, oldHash, 0) != 0 {
		t.Fatalf("image: %+v, new %d, old %d", res, thumbsPresent(t, svc, newHash, 0), thumbsPresent(t, svc, oldHash, 0))
	}
	if n, _, _ := svc.fileCache.(statSizer).StatSize(CacheKey(newHash, "large", 0)); n != 0 {
		t.Fatalf("the empty marker became %d bytes", n)
	}

	// Run again: nothing left to move.
	if res, err := RekeyFile(img, info, 4096, false); err != nil || res != (RekeyResult{}) {
		t.Fatalf("second run: %+v, %v", res, err)
	}

	// A video: every seek percentage moves.
	vid := filepath.Join(dir, "v.mp4")
	writeFile(t, vid)
	vinfo, err := os.Stat(vid)
	if err != nil {
		t.Fatal(err)
	}
	vOld := oldPathHash(vid, 4096, vinfo.ModTime())
	storeThumbs(t, svc, vOld, 0, 1, 50, 100)
	res, err = RekeyFile(vid, vinfo, 4096, true)
	if err != nil {
		t.Fatal(err)
	}
	vNew, _ := mustFileKey(t, vid)
	if res.Moved != 8 || thumbsPresent(t, svc, vNew, 0, 1, 50, 100) != 8 || thumbsPresent(t, svc, vOld, 0, 1, 50, 100) != 0 {
		t.Fatalf("video: %+v", res)
	}

	// A second link of the same file whose old entries are still there: the new key already
	// has entries (from the first link), so the old ones are deleted and counted.
	link := filepath.Join(dir, "shortcut.mp4")
	if err := os.Link(vid, link); err != nil {
		t.Fatal(err)
	}
	linfo, err := os.Stat(link)
	if err != nil {
		t.Fatal(err)
	}
	lOld := oldPathHash(link, 4096, linfo.ModTime())
	storeThumbs(t, svc, lOld, 0)
	res, err = RekeyFile(link, linfo, 4096, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved != 0 || res.Dropped != 2 || res.Freed != 2*int64(len("thumbnail bytes")) || thumbsPresent(t, svc, lOld, 0) != 0 {
		t.Fatalf("second link: %+v", res)
	}

	// A different size in the old key finds nothing.
	if res, err := RekeyFile(img, info, 8192, false); err != nil || res != (RekeyResult{}) {
		t.Fatalf("wrong size: %+v, %v", res, err)
	}
}
