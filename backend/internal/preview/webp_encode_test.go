package preview

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"net/http"
	"testing"

	"github.com/gen2brain/webp"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	xwebp "golang.org/x/image/webp"
)

// noisyImage returns a w×h image with varied pixels (so it doesn't compress to almost nothing)
// and, if alpha is true, a transparent left half.
func noisyImage(w, h int, alpha bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	seed := uint32(12345)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223
			a := uint8(255)
			if alpha && x < w/2 {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(seed >> 24), uint8(x), uint8(y), a})
		}
	}
	return img
}

func encodeAs(t *testing.T, img image.Image, format string) []byte {
	t.Helper()
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	case "tiff":
		err = tiff.Encode(&buf, img, nil)
	case "bmp":
		err = bmp.Encode(&buf, img)
	case "webp":
		err = webp.Encode(&buf, img, webp.Options{Lossless: true})
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return buf.Bytes()
}

func isWebP(b []byte) bool {
	return len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP"
}

// TestThumbnailsOfNonJPEGImagesAreWebP resizes each non-JPEG source format through the real
// preview path and checks the result is a valid WebP, decoded by the independent x/image decoder.
func TestThumbnailsOfNonJPEGImagesAreWebP(t *testing.T) {
	svc := NewPreviewGenerator(2, t.TempDir())
	sizes := map[string][2]int{"small": {256, 256}, "large": {640, 640}}
	for _, src := range []struct {
		ext   string
		alpha bool
	}{{".png", true}, {".png", false}, {".gif", false}, {".tiff", false}, {".bmp", false}, {".webp", true}} {
		in := encodeAs(t, noisyImage(1200, 800, src.alpha), src.ext[1:])
		format, err := svc.FormatFromExtension(src.ext)
		if err != nil {
			t.Fatalf("%s: %v", src.ext, err)
		}
		for name, wh := range sizes {
			opts, err := getPreviewOptions(name)
			if err != nil {
				t.Fatal(err)
			}
			opts.Format = format
			out, err := svc.CreatePreview(context.Background(), bytes.NewReader(in), int64(len(in)), opts)
			if err != nil {
				t.Fatalf("%s %s: %v", src.ext, name, err)
			}
			if !isWebP(out) {
				t.Fatalf("%s %s: thumbnail is not WebP (starts %q)", src.ext, name, out[:12])
			}
			got, err := xwebp.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("%s %s: x/image cannot decode the thumbnail: %v", src.ext, name, err)
			}
			b := got.Bounds()
			if b.Dx() > wh[0] || b.Dy() > wh[1] || (name == "small" && (b.Dx() != 256 || b.Dy() != 256)) {
				t.Fatalf("%s %s: unexpected size %dx%d", src.ext, name, b.Dx(), b.Dy())
			}
			if src.alpha {
				if _, _, _, a := got.At(1, b.Dy()/2).RGBA(); a != 0 {
					t.Fatalf("%s %s: transparency lost (alpha %d on the transparent side)", src.ext, name, a)
				}
			}
			if _, _, _, a := got.At(b.Dx()-2, b.Dy()/2).RGBA(); a != 0xffff {
				t.Fatalf("%s %s: opaque side not opaque (alpha %d)", src.ext, name, a)
			}
			if ct := http.DetectContentType(out); ct != "image/webp" {
				t.Fatalf("%s %s: Content-Type sniffed as %q", src.ext, name, ct)
			}
		}
	}
}

// TestJPEGThumbnailsStayJPEG guards the scope of the change: JPEG sources keep JPEG thumbnails.
func TestJPEGThumbnailsStayJPEG(t *testing.T) {
	if got := thumbnailFormat(FormatJpeg); got != FormatJpeg {
		t.Fatalf("JPEG maps to %v", got)
	}
	if got := thumbnailFormat(FormatHeic); got != FormatHeic {
		t.Fatalf("HEIC maps to %v", got)
	}
}

// TestUniformImageFallsBackToJPEG: a flat image compresses below minPreviewSize as WebP, which
// the pipeline would reject as a failed preview, so it must come out as a usable JPEG.
func TestUniformImageFallsBackToJPEG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var raw bytes.Buffer
	if err := safeWebPEncode(&raw, img, QualityLow.webpQuality()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := encodeWebP(&out, img, QualityLow); err != nil {
		t.Fatal(err)
	}
	if out.Len() < minPreviewSize {
		t.Fatalf("output %d bytes is below minPreviewSize %d", out.Len(), minPreviewSize)
	}
	if raw.Len() >= minPreviewSize {
		t.Fatalf("test image encodes to %d bytes of WebP; it must be under %d to exercise the fallback", raw.Len(), minPreviewSize)
	}
	if ct := http.DetectContentType(out.Bytes()); ct != "image/jpeg" {
		t.Fatalf("tiny WebP (%d bytes) did not fall back to JPEG (got %s)", raw.Len(), ct)
	}
}

// TestWebPDecodingStaysWithXImage pins decision 10: importing the encoder must not make it the
// process-wide WebP decoder. x/image rejects animated WebP; gen2brain's decoder accepts it, so a
// successful image.Decode of an animated WebP means gen2brain is registered.
func TestWebPDecodingStaysWithXImage(t *testing.T) {
	f1 := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	f2 := noisyImage(4, 4, false)
	var anim bytes.Buffer
	if err := webp.EncodeAll(&anim, &webp.WEBP{Image: []image.Image{f1, f2}, Delay: []int{100, 100}}, webp.Options{Quality: 75}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := image.Decode(bytes.NewReader(anim.Bytes())); err == nil {
		t.Fatal("image.Decode accepted an animated WebP: gen2brain/webp is registered as the WebP decoder")
	}
	// Lossless WebP must decode exactly (x/image returns NRGBA; gen2brain would return 4:2:0 YCbCr).
	still := encodeAs(t, noisyImage(32, 32, false), "webp")
	img, format, err := image.Decode(bytes.NewReader(still))
	if err != nil || format != "webp" {
		t.Fatalf("still WebP: format %q, err %v", format, err)
	}
	if _, ok := img.(*image.NRGBA); !ok {
		t.Fatalf("lossless WebP decoded as %T, want *image.NRGBA (x/image)", img)
	}
}

// TestWebPEncodeIsPanicSafe: a panic inside the encoder becomes an error.
func TestWebPEncodeIsPanicSafe(t *testing.T) {
	if err := safeWebPEncode(io.Discard, panicImage{}, 75); err == nil {
		t.Fatal("expected an error from a panicking image")
	}
}

type panicImage struct{}

func (panicImage) ColorModel() color.Model { return color.NRGBAModel }
func (panicImage) Bounds() image.Rectangle { return image.Rect(0, 0, 8, 8) }
func (panicImage) At(x, y int) color.Color { panic("boom") }
