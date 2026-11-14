//go:build !windows

package preview

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
)

// storeThumbs puts a value under each size at each pct for hash.
func storeThumbs(t *testing.T, svc *Service, hash string, pcts ...int) {
	t.Helper()
	for _, pct := range pcts {
		for _, size := range []string{"small", "large"} {
			if err := svc.fileCache.Store(context.Background(), CacheKey(hash, size, pct), []byte("thumbnail bytes")); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// thumbsPresent counts the entries of hash present at the given pcts (both sizes).
func thumbsPresent(t *testing.T, svc *Service, hash string, pcts ...int) int {
	t.Helper()
	n := 0
	for _, pct := range pcts {
		for _, size := range []string{"small", "large"} {
			if _, found, err := svc.fileCache.(statSizer).StatSize(CacheKey(hash, size, pct)); err != nil {
				t.Fatal(err)
			} else if found {
				n++
			}
		}
	}
	return n
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("file content"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Deleting one of two hard links keeps the thumbnails (the other link still shows them);
// deleting the last link deletes both sizes and, for a video, every seek percentage.
func TestThumbsBeforeDeleteLastLinkOnly(t *testing.T) {
	svc := NewPreviewGenerator(1, t.TempDir())
	useService(t, svc)
	ctx := context.Background()
	dir := t.TempDir()
	a := filepath.Join(dir, "post", "a.mp4")
	b := filepath.Join(dir, "links", "a.mp4")
	writeFile(t, a)
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	hash, _ := mustFileKey(t, a)
	storeThumbs(t, svc, hash, 0, 25, 50, 75, 100)

	thumbs := ThumbsBeforeDelete(b, true)
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	thumbs.Delete(ctx)
	if n := thumbsPresent(t, svc, hash, 0, 25, 50, 75, 100); n != 10 {
		t.Fatalf("after deleting one of two links: %d of 10 entries left", n)
	}

	thumbs = ThumbsBeforeDelete(a, true)
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	thumbs.Delete(ctx)
	if n := thumbsPresent(t, svc, hash, 0, 25, 50, 75, 100); n != 0 {
		t.Fatalf("after deleting the last link: %d entries left", n)
	}
}

// A non-video's last link deletes both sizes at seek 0 (upstream deleted the large one only
// when deleting the small one had failed).
func TestThumbsBeforeDeleteBothSizes(t *testing.T) {
	svc := NewPreviewGenerator(1, t.TempDir())
	useService(t, svc)
	a := filepath.Join(t.TempDir(), "a.jpg")
	writeFile(t, a)
	hash, _ := mustFileKey(t, a)
	storeThumbs(t, svc, hash, 0)
	thumbs := ThumbsBeforeDelete(a, false)
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	thumbs.Delete(context.Background())
	if n := thumbsPresent(t, svc, hash, 0); n != 0 {
		t.Fatalf("%d entries left", n)
	}
}

// Deleting a symlink removes the link, not the file it points to: the target keeps its
// thumbnails. So do directories, a missing path, and the zero ThumbDeletion.
func TestThumbsBeforeDeleteKeeps(t *testing.T) {
	svc := NewPreviewGenerator(1, t.TempDir())
	useService(t, svc)
	ctx := context.Background()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.jpg")
	writeFile(t, target)
	hash, _ := mustFileKey(t, target)
	storeThumbs(t, svc, hash, 0)

	link := filepath.Join(dir, "link.jpg")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	thumbs := ThumbsBeforeDelete(link, false)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	thumbs.Delete(ctx)
	ThumbsBeforeDelete(dir, false).Delete(ctx)
	ThumbsBeforeDelete(filepath.Join(dir, "missing.jpg"), false).Delete(ctx)
	ThumbDeletion{}.Delete(ctx)
	if n := thumbsPresent(t, svc, hash, 0); n != 2 {
		t.Fatalf("%d of 2 entries left", n)
	}
}

// A move keeps the thumbnails where browsing the new path looks for them.
func TestMovedFileKeepsThumbnails(t *testing.T) {
	svc := NewPreviewGenerator(1, t.TempDir())
	useService(t, svc)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jpg")
	writeFile(t, a)
	hash, _ := mustFileKey(t, a)
	storeThumbs(t, svc, hash, 0)
	moved := filepath.Join(dir, "elsewhere", "b.jpg")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(a, moved); err != nil {
		t.Fatal(err)
	}
	var f iteminfo.ExtendedFileInfo
	f.RealPath = moved
	got, err := cacheHashFor(f)
	if err != nil {
		t.Fatal(err)
	}
	if got != hash || thumbsPresent(t, svc, got, 0) != 2 {
		t.Fatalf("moved file: key %s (was %s), entries %d", got, hash, thumbsPresent(t, svc, got, 0))
	}
}

// DelThumbs (an upload replacing the content) deletes the current entries, both sizes and a
// video's seek percentages, whatever the link count.
func TestDelThumbsOnOverwrite(t *testing.T) {
	svc := NewPreviewGenerator(1, t.TempDir())
	useService(t, svc)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mp4")
	writeFile(t, a)
	if err := os.Link(a, filepath.Join(dir, "b.mp4")); err != nil {
		t.Fatal(err)
	}
	hash, _ := mustFileKey(t, a)
	storeThumbs(t, svc, hash, 0, 50)
	var f iteminfo.ExtendedFileInfo
	f.RealPath = a
	f.Type = "video/mp4"
	DelThumbs(context.Background(), f)
	if n := thumbsPresent(t, svc, hash, 0, 50); n != 0 {
		t.Fatalf("%d entries left", n)
	}
}
