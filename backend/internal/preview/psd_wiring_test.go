package preview

import (
	"bytes"
	"context"
	"image"
	"testing"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
)

// psdFromImage builds a PSD (RLE) whose flattened image is img; with alpha, channel 3 is its
// transparency (negative layer count).
func psdFromImage(img *image.NRGBA, alpha bool, layerPad int) []byte {
	b := img.Bounds()
	ch := 3
	if alpha {
		ch = 4
	}
	planes := make([][]byte, ch)
	for c := range planes {
		planes[c] = make([]byte, b.Dx()*b.Dy())
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			o := img.PixOffset(x, y)
			for c := 0; c < ch; c++ {
				planes[c][y*b.Dx()+x] = img.Pix[o+c]
			}
		}
	}
	layers := 3
	if alpha {
		layers = -3
	}
	return buildPSD(psdSpec{w: b.Dx(), h: b.Dy(), ch: ch, layers: layers, comp: 1, planes: planes, layerPad: layerPad})
}

func previewConfig(t *testing.T, data []byte) (image.Config, string) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	return cfg, format
}

func TestPSDPreviewSizes(t *testing.T) {
	dir := t.TempDir()
	useService(t, NewPreviewGenerator(2, t.TempDir()))
	wide := writeImageFile(t, dir, "wide.psd", iteminfo.PSDMimeType, psdFromImage(noisyImage(5000, 40, false), false, 0))
	cases := []struct {
		size           string
		exactW, exactH int
		format         string
	}{
		{"small", 256, 256, "webp"}, // fill: cropped to the box
		{"large", 640, 5, "webp"},
		{"xlarge", 1024, 8, "webp"},
		{"original", 4096, 32, "jpeg"}, // 32.8, floored by imaging.Fit
		{"full", 5000, 40, "jpeg"},
	}
	for _, c := range cases {
		data, err := GetPreviewForFile(context.Background(), wide, c.size, "", 0)
		if err != nil {
			t.Fatalf("%s: %v", c.size, err)
		}
		cfg, format := previewConfig(t, data)
		if format != c.format || cfg.Width != c.exactW || cfg.Height != c.exactH {
			t.Errorf("%s: %s %dx%d, want %s %dx%d", c.size, format, cfg.Width, cfg.Height, c.format, c.exactW, c.exactH)
		}
	}

	// Wider than WebP would allow: "full" (JPEG) keeps every pixel.
	huge := writeImageFile(t, dir, "huge.psb", iteminfo.PSDMimeType, psdFromImage(noisyImage(17000, 4, false), false, 0))
	data, err := GetPreviewForFile(context.Background(), huge, "full", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _ := previewConfig(t, data); cfg.Width != 17000 {
		t.Errorf("full of a 17000 px PSD is %d wide, want 17000", cfg.Width)
	}
}

func TestPSDFullBlock(t *testing.T) {
	cases := []struct{ w, h, want int }{
		{10000, 6000, 1},  // 60 MP
		{4576, 22559, 1},  // 103 MP
		{16000, 12000, 2}, // 192 MP: over 128 MP
		{70000, 100, 1},   // over JPEG's 65,535 px side: decoded whole, then fitted
	}
	for _, c := range cases {
		if got := psdFullBlock(c.w, c.h); got != c.want {
			t.Errorf("psdFullBlock(%d, %d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}
}

// The viewer sizes are JPEG: transparent areas become white, not black.
func TestPSDViewerFlattensOnWhite(t *testing.T) {
	dir := t.TempDir()
	useService(t, NewPreviewGenerator(2, t.TempDir()))
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 200, 30, 30, 0 // red, fully transparent
	}
	for y := 0; y < 32; y++ { // top half opaque red
		for x := 0; x < 64; x++ {
			img.Pix[img.PixOffset(x, y)+3] = 255
		}
	}
	file := writeImageFile(t, dir, "clear.psd", iteminfo.PSDMimeType, psdFromImage(img, true, 0))
	data, err := GetPreviewForFile(context.Background(), file, "original", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := got.At(32, 56).RGBA()
	if r>>8 < 245 || g>>8 < 245 || b>>8 < 245 {
		t.Errorf("transparent area = %d,%d,%d, want white", r>>8, g>>8, b>>8)
	}
	r, g, _, _ = got.At(32, 8).RGBA()
	if r>>8 < 180 || g>>8 > 60 {
		t.Errorf("opaque area = %d,%d, want red", r>>8, g>>8)
	}
}

// A layered PSD over the 75 MiB image limit still gets previews, from its flattened image.
func TestPSDPreviewOverSizeLimit(t *testing.T) {
	dir := t.TempDir()
	useService(t, NewPreviewGenerator(2, t.TempDir()))
	data := psdFromImage(noisyImage(800, 600, true), true, iteminfo.LargeFileSizeThreshold+1)
	file := writeImageFile(t, dir, "layered.psd", iteminfo.PSDMimeType, data)
	if file.Size <= iteminfo.LargeFileSizeThreshold {
		t.Fatalf("test file is %d bytes, must be over the limit", file.Size)
	}
	if got := determinePreviewType(file); got != previewTypePSD {
		t.Fatalf("determinePreviewType = %v, want previewTypePSD", got)
	}
	for _, size := range []string{"small", "large", "original"} {
		if _, err := GetPreviewForFile(context.Background(), file, size, "", 0); err != nil {
			t.Fatalf("%s: %v", size, err)
		}
	}
	if !decodeOnceEligible(file, []string{"small", "large"}) || decodeOnceEligible(file, []string{"small", "original"}) {
		t.Fatal("decodeOnceEligible: want true for thumbnail sizes only")
	}
}

// A damaged PSD fails without caching anything.
func TestPSDPreviewDamaged(t *testing.T) {
	dir := t.TempDir()
	svc := NewPreviewGenerator(2, t.TempDir())
	useService(t, svc)
	full := psdFromImage(noisyImage(300, 200, false), false, 1000)
	file := writeImageFile(t, dir, "cut.psd", iteminfo.PSDMimeType, full[:len(full)/3])
	if _, err := GetPreviewForFile(context.Background(), file, "large", "", 0); err == nil {
		t.Fatal("damaged PSD: no error")
	}
	if res, err := Pregenerate(context.Background(), file, []string{"small", "large"}); err == nil {
		t.Fatalf("Pregenerate of a damaged PSD = %+v, want an error", res)
	}
}
