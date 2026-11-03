package preview

// fufu: thumbnails of non-JPEG still images (PNG, GIF, TIFF, BMP, WebP, netpbm) are encoded
// as lossy WebP instead of in the source format: it keeps transparency and is a fraction of
// the size of a PNG thumbnail, and TIFF thumbnails become displayable in every browser.
//
// The encoder is github.com/gen2brain/webp (libwebp compiled to Go), used through the trimmed
// copy in third_party/gen2brain-webp, which does not register itself as the process-wide WebP
// decoder. Decoding WebP sources therefore stays with golang.org/x/image/webp.

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"

	"github.com/gen2brain/webp"
)

// webpEncodeMethod is libwebp's speed/size trade-off (0 fastest .. 6 smallest; its default is 4).
// For 640 px screenshot thumbnails, 2 encodes about 3x faster than 4 for about 3% larger files.
const webpEncodeMethod = 2

// thumbnailFormat returns the format a preview is encoded in for the given source format.
func thumbnailFormat(source Format) Format {
	switch source {
	case FormatPng, FormatGif, FormatTiff, FormatBmp, FormatWebp,
		FormatPbm, FormatPgm, FormatPpm, FormatPam:
		return FormatWebp
	default:
		return source
	}
}

// webpQuality returns the lossy WebP quality (0-100) for a preview quality level.
func (x Quality) webpQuality() int {
	switch x {
	case QualityHigh:
		return 85
	case QualityMedium:
		return 75
	case QualityLow:
		return 65
	default:
		return 75
	}
}

// encodeWebP writes img as lossy WebP. A nearly uniform image can compress below
// minPreviewSize, which the preview pipeline treats as a failed preview; those are written as
// JPEG instead (losing transparency, which such an image hardly has).
func encodeWebP(out io.Writer, img image.Image, quality Quality) error {
	var buf bytes.Buffer
	if err := safeWebPEncode(&buf, img, quality.webpQuality()); err != nil {
		return err
	}
	if buf.Len() < minPreviewSize {
		return jpeg.Encode(out, img, &jpeg.Options{Quality: quality.jpegQuality()})
	}
	_, err := out.Write(buf.Bytes())
	return err
}

// safeWebPEncode turns a panic inside the generated libwebp code into an error, so one bad
// image fails its own preview instead of taking the request (or a job) down.
func safeWebPEncode(w io.Writer, img image.Image, quality int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("webp encode panicked: %v", r)
		}
	}()
	if err := webp.Encode(w, img, webp.Options{Quality: quality, Method: webpEncodeMethod}); err != nil {
		return fmt.Errorf("webp encode: %w", err)
	}
	return nil
}
