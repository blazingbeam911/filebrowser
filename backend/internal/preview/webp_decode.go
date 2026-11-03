package preview

// fufu: WebP sources are decoded by github.com/deepteams/webp (third_party/deepteams-webp),
// which registers itself with image.RegisterFormat ahead of golang.org/x/image/webp. Go uses
// the first registered decoder whose signature matches, so image.Decode and imaging.Decode
// (for still WebP) go through it: about 2.5x faster than x/image on lossless WebP, identical
// output, and libwebp-accurate colour on lossy WebP.

import (
	"bytes"
	"image"

	_ "github.com/deepteams/webp" // registers the WebP decoder
	"github.com/gen2brain/webp"

	"github.com/gtsteffaniak/go-logger/logger"
)

// activeWebPDecoder reports which decoder image.Decode uses for WebP. A lossy WebP decodes to
// *image.NRGBA with deepteams/webp and to *image.YCbCr with x/image/webp.
func activeWebPDecoder() string {
	src := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for i := range src.Pix {
		src.Pix[i] = byte(i * 7)
		if i%4 == 3 {
			src.Pix[i] = 255 // opaque
		}
	}
	var buf bytes.Buffer
	if err := webp.Encode(&buf, src, webp.Options{Quality: 75}); err != nil {
		return "unknown (test encode failed: " + err.Error() + ")"
	}
	img, _, err := image.Decode(bytes.NewReader(buf.Bytes()))
	switch {
	case err != nil:
		return "none (" + err.Error() + ")"
	case func() bool { _, ok := img.(*image.NRGBA); return ok }():
		return "deepteams/webp"
	case func() bool {
		switch img.(type) {
		case *image.YCbCr, *image.NYCbCrA:
			return true
		}
		return false
	}():
		return "x/image/webp"
	default:
		return "unknown"
	}
}

// logWebPDecoder logs the active WebP decoder once at startup, as a warning if it isn't
// deepteams/webp.
func logWebPDecoder() {
	if d := activeWebPDecoder(); d == "deepteams/webp" {
		logger.Infof("WebP decoder: %s", d)
	} else {
		logger.Warningf("WebP decoder: %s (expected deepteams/webp; WebP thumbnails will be slower)", d)
	}
}
