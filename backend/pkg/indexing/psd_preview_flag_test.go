package indexing

import (
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
)

// fufu: PSDs get previews whatever their size (internal/preview/psd.go reads only the flattened
// image), and rows stored before that (no preview, or an old type for .psb) are recomputed.
func TestPSDPreviewFlags(t *testing.T) {
	big := int64(iteminfo.LargeFileSizeThreshold + 1)
	psd := &iteminfo.ItemInfo{Name: "a.psd", Type: iteminfo.PSDMimeType, Size: big}
	setFilePreviewFlags(psd, "/nonexistent/a.psd")
	if !psd.HasPreview {
		t.Error("PSD over the size limit: HasPreview = false, want true")
	}
	png := &iteminfo.ItemInfo{Name: "a.png", Type: "image/png", Size: big}
	setFilePreviewFlags(png, "/nonexistent/a.png")
	if png.HasPreview {
		t.Error("PNG over the size limit: HasPreview = true, want false")
	}

	idx, _, cleanup := setupTestIndex(t)
	defer cleanup()
	now := time.Now()
	opts := Options{Recursive: false, SkipExtendedAttrs: false}
	for _, c := range []struct{ name, storedType string }{
		{"layered.psd", iteminfo.PSDMimeType},
		{"layered.psb", "application/octet-stream"},
	} {
		file := &mockFileInfo{name: c.name, size: 1024, modTime: now}
		cached := map[string]*iteminfo.ItemInfo{
			c.name: {Name: c.name, Size: 1024, ModTime: now, Type: c.storedType, HasPreview: false},
		}
		item, _, _, _ := idx.processFileItem(file, "/test/", opts, nil, cached)
		if item.Type != iteminfo.PSDMimeType || !item.HasPreview {
			t.Errorf("%s (stored %s, no preview): got type %s, HasPreview %v; want %s, true",
				c.name, c.storedType, item.Type, item.HasPreview, iteminfo.PSDMimeType)
		}
	}
}
