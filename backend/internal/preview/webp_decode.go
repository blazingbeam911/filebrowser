package preview

// fufu: WebP sources are decoded by github.com/deepteams/webp (third_party/deepteams-webp),
// which registers itself with image.RegisterFormat ahead of golang.org/x/image/webp. Go uses
// the first registered decoder whose signature matches, so image.Decode and imaging.Decode
// (for still WebP) go through it: about 2.5x faster than x/image on lossless WebP, identical
// output, and libwebp-accurate colour on lossy WebP.

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"

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

// decodeFirstWebPFrame decodes only the first frame of an animated WebP and places it on the
// canvas at its offset, as imaging does with frame 0. imaging.Decode would decode every frame
// (image_decode.go). deepteams/webp's registered decoder reads the first frame of an animated
// file; x/image/webp can't read animated files at all.
func decodeFirstWebPFrame(data []byte, canvasW, canvasH int) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	x, y := firstWebPFrameOffset(data)
	b := img.Bounds()
	if x == 0 && y == 0 && b.Min == (image.Point{}) && b.Dx() == canvasW && b.Dy() == canvasH {
		return img, nil
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, canvasW, canvasH))
	draw.Draw(canvas, image.Rect(x, y, x+b.Dx(), y+b.Dy()), img, b.Min, draw.Src)
	return canvas, nil
}

// firstWebPFrameOffset returns where the first animation frame (ANMF chunk) of a WebP file
// sits on the canvas: the chunk stores X/2 and Y/2 as 24-bit little-endian numbers.
func firstWebPFrameOffset(data []byte) (x, y int) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0
	}
	le24 := func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }
	for i := 12; i+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if string(data[i:i+4]) == "ANMF" {
			if i+14 > len(data) {
				return 0, 0
			}
			return 2 * le24(data[i+8:i+11]), 2 * le24(data[i+11:i+14])
		}
		i += 8 + size + size&1
	}
	return 0, 0
}
