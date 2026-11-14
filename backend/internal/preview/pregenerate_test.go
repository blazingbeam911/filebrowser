package preview

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
)

// useService makes svc the package's preview service for one test.
func useService(t *testing.T, svc *Service) {
	t.Helper()
	old := service
	service = svc
	t.Cleanup(func() { service = old })
}

// writeImageFile writes data to name in dir and returns its file info as browsing builds it.
func writeImageFile(t *testing.T, dir, name, mime string, data []byte) iteminfo.ExtendedFileInfo {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	var f iteminfo.ExtendedFileInfo
	f.Name = name
	f.Size = st.Size()
	f.ModTime = st.ModTime()
	f.Type = mime
	f.HasPreview = true
	f.RealPath = p
	return f
}

func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, noisyImage(w, h, false), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// mustCacheHash is cacheHashFor for a file that must be readable.
func mustCacheHash(t *testing.T, file iteminfo.ExtendedFileInfo) string {
	t.Helper()
	hash, err := cacheHashFor(file)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// cachedBytes returns what the cache holds for file at size (seek 0).
func cachedBytes(t *testing.T, svc *Service, file iteminfo.ExtendedFileInfo, size string) []byte {
	t.Helper()
	data, found, err := svc.fileCache.Load(context.Background(), CacheKey(mustCacheHash(t, file), size, 0))
	if err != nil || !found {
		t.Fatalf("%s %s: not in the cache (%v)", file.Name, size, err)
	}
	return data
}

// The job's thumbnails are the bytes browsing would make, stored where browsing looks, for
// images taken through the single decode and for ones taken through upstream's path.
func TestPregenerateMatchesBrowsing(t *testing.T) {
	src := t.TempDir()
	sizes := []string{"small", "large"}
	cases := []struct {
		name, mime string
		data       []byte
		decodeOnce bool
	}{
		{"big.png", "image/png", encodeAs(t, noisyImage(1600, 1000, true), "png"), true},
		{"big.webp", "image/webp", encodeAs(t, noisyImage(1600, 1000, false), "webp"), true},
		{"big.gif", "image/gif", encodeAs(t, noisyImage(1600, 1000, false), "gif"), true},
		{"big.jpg", "image/jpeg", encodeJPEG(t, 1600, 1000), true},
		{"small-file.png", "image/png", encodeAs(t, noisyImage(300, 200, false), "png"), false}, // under 256 KB
		{"fits-large.png", "image/png", encodeAs(t, noisyImage(600, 400, false), "png"), false}, // within 640×640
		// fufu: PSDs, from one decode of the flattened image (psd.go), even when small.
		{"big.psd", iteminfo.PSDMimeType, psdFromImage(noisyImage(1600, 1000, true), true, 0), true},
		{"small.psd", iteminfo.PSDMimeType, psdFromImage(noisyImage(300, 200, false), false, 0), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := writeImageFile(t, src, c.name, c.mime, c.data)
			if c.decodeOnce && file.Size < 256*1024 && file.Type != iteminfo.PSDMimeType {
				t.Fatalf("test image is %d bytes, must be at least 256 KB to be resized", file.Size)
			}

			jobSvc := NewPreviewGenerator(2, t.TempDir())
			useService(t, jobSvc)
			if got := decodeOnceEligible(file, sizes); got != c.decodeOnce {
				t.Fatalf("decodeOnceEligible = %v, want %v", got, c.decodeOnce)
			}
			res, err := Pregenerate(context.Background(), file, sizes)
			if err != nil || res.Generated != 2 || res.Cached != 0 {
				t.Fatalf("first run = %+v, %v; want 2 generated", res, err)
			}
			res, err = Pregenerate(context.Background(), file, sizes)
			if err != nil || res.Generated != 0 || res.Cached != 2 {
				t.Fatalf("second run = %+v, %v; want 2 cached", res, err)
			}

			browseSvc := NewPreviewGenerator(2, t.TempDir())
			useService(t, browseSvc)
			for _, size := range sizes {
				want, err := GetPreviewForFile(context.Background(), file, size, "", 0)
				if err != nil {
					t.Fatalf("browse %s: %v", size, err)
				}
				if got := cachedBytes(t, jobSvc, file, size); !bytes.Equal(got, want) {
					t.Errorf("%s: job made %d bytes, browsing %d, not the same", size, len(got), len(want))
				}
			}

			// And a browse request served from the job's cache makes nothing new.
			useService(t, jobSvc)
			before := countFiles(t, jobSvc.cacheDir)
			for _, size := range sizes {
				if _, err := GetPreviewForFile(context.Background(), file, size, "", 0); err != nil {
					t.Fatal(err)
				}
			}
			if after := countFiles(t, jobSvc.cacheDir); after != before {
				t.Errorf("browsing after the job added %d cache files", after-before)
			}
		})
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A size already cached is left alone and only the missing one is made.
func TestPregenerateOnlyMissingSizes(t *testing.T) {
	svc := NewPreviewGenerator(2, t.TempDir())
	useService(t, svc)
	file := writeImageFile(t, t.TempDir(), "a.png", "image/png", encodeAs(t, noisyImage(1600, 1000, false), "png"))
	if _, err := GetPreviewForFile(context.Background(), file, "small", "", 0); err != nil {
		t.Fatal(err)
	}
	res, err := Pregenerate(context.Background(), file, []string{"small", "large"})
	if err != nil || res.Generated != 1 || res.Cached != 1 {
		t.Fatalf("got %+v, %v; want 1 generated, 1 cached", res, err)
	}
	cachedBytes(t, svc, file, "large")
}

// A file whose image can't be decoded fails and leaves nothing in the cache; one that failed
// before (the empty marker) is reported without another attempt.
func TestPregenerateFailures(t *testing.T) {
	svc := NewPreviewGenerator(2, t.TempDir())
	useService(t, svc)
	dir := t.TempDir()

	good := encodeAs(t, noisyImage(1600, 1000, false), "png")
	broken := append([]byte{}, good[:200]...) // header (and dimensions) intact, data cut off
	broken = append(broken, bytes.Repeat([]byte{0x55}, 400*1024)...)
	file := writeImageFile(t, dir, "broken.png", "image/png", broken)
	if !decodeOnceEligible(file, []string{"small", "large"}) {
		t.Fatal("broken.png should reach the single decode")
	}
	before := countFiles(t, svc.cacheDir)
	if _, err := Pregenerate(context.Background(), file, []string{"small", "large"}); err == nil {
		t.Fatal("broken image: no error")
	}
	if after := countFiles(t, svc.cacheDir); after != before {
		t.Errorf("broken image: %d cache files added", after-before)
	}

	marked := writeImageFile(t, dir, "marked.png", "image/png", good)
	if err := svc.fileCache.Store(context.Background(), CacheKey(mustCacheHash(t, marked), "small", 0), []byte{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Pregenerate(context.Background(), marked, []string{"small", "large"}); !errors.Is(err, ErrPreviewTooSmall) {
		t.Fatalf("marked file: err = %v, want ErrPreviewTooSmall", err)
	}

	notPreviewable := marked
	notPreviewable.HasPreview = false
	if _, err := Pregenerate(context.Background(), notPreviewable, []string{"small"}); !errors.Is(err, ErrUnsupportedMedia) {
		t.Fatalf("no preview: err = %v, want ErrUnsupportedMedia", err)
	}
}

// A cancelled context stops before any work.
func TestPregenerateCancelled(t *testing.T) {
	svc := NewPreviewGenerator(2, t.TempDir())
	useService(t, svc)
	file := writeImageFile(t, t.TempDir(), "a.png", "image/png", encodeAs(t, noisyImage(1600, 1000, false), "png"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := Pregenerate(ctx, file, []string{"small", "large"}); err == nil {
		t.Fatal("cancelled context: no error")
	}
}
