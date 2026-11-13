package preview

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/kovidgoyal/imaging"
	"github.com/kovidgoyal/imaging/prism/meta/autometa"
)

// maxPreviewPixels is the most pixels a preview may decode: the canvas of an image, or
// canvas × frames for an animated WebP, which is decoded in full. Decoding takes about
// 2 bytes per pixel for JPEG and 3.5–4.5 for PNG and WebP, and several previews are made
// at once, so this keeps a single image from using up the server's memory. Larger images
// get no preview.
const maxPreviewPixels = 200_000_000

// ErrImageTooLarge means an image has more pixels than a preview may decode.
var ErrImageTooLarge = errors.New("image too large for preview")

func errImageTooLarge(width, height uint32, frames int) error {
	if frames > 1 {
		return fmt.Errorf("%w: %dx%d, %d frames (limit %d pixels)", ErrImageTooLarge, width, height, frames, maxPreviewPixels)
	}
	return fmt.Errorf("%w: %dx%d (limit %d pixels)", ErrImageTooLarge, width, height, maxPreviewPixels)
}

// decodePreviewImage decodes the image a preview is made from. format is the preview's
// output format; it picks the fallback decoder.
//
// imaging.Decode decodes every frame of an animated GIF, PNG (APNG) or WebP and then
// returns only the first, so its memory use grows with the number of frames. The header
// is therefore read first:
//   - images larger than maxPreviewPixels are refused with ErrImageTooLarge;
//   - GIFs (imaging treats every GIF as animated) are decoded with image/gif, which reads
//     only the first frame, giving the same image imaging returns;
//   - animated PNGs are decoded with image/png, which reads only the default image;
//   - animated WebPs are decoded by imaging while canvas × frames is within
//     maxPreviewPixels (the standard WebP decoder can't read animated files);
//   - everything else is decoded as before: imaging first, then the standard decoder.
func decodePreviewImage(r io.Reader, format Format) (image.Image, error) {
	md, stream, err := autometa.Load(r)
	if err == nil && md != nil {
		pixels := int64(md.PixelWidth) * int64(md.PixelHeight)
		if pixels > maxPreviewPixels {
			return nil, errImageTooLarge(md.PixelWidth, md.PixelHeight, 1)
		}
		if md.HasFrames {
			switch md.Format {
			case imaging.GIF:
				img, err := gif.Decode(stream)
				if err != nil {
					return nil, fmt.Errorf("failed to decode image: %w", err)
				}
				return imaging.NormalizeOrigin(img), nil
			case imaging.PNG:
				img, err := png.Decode(stream)
				if err != nil {
					return nil, fmt.Errorf("failed to decode image: %w", err)
				}
				return img, nil
			case imaging.WEBP:
				data, err := io.ReadAll(stream)
				if err != nil {
					return nil, fmt.Errorf("failed to read image: %w", err)
				}
				frames := max(countWebPFrames(data), 1)
				if pixels*int64(frames) > maxPreviewPixels {
					return nil, errImageTooLarge(md.PixelWidth, md.PixelHeight, frames)
				}
				stream = bytes.NewReader(data)
			}
		}
	}
	if stream == nil {
		stream = r
	}

	// Try the imaging library first, then the format-specific standard decoder.
	img, err := imaging.Decode(stream, previewDecodeOpts(true)...)
	if err == nil {
		return img, nil
	}
	if seeker, ok := stream.(io.Seeker); ok {
		_, _ = seeker.Seek(0, io.SeekStart)
	}
	if format == FormatJpeg {
		img, err = jpeg.Decode(stream)
		if err != nil {
			// JPEG-specific error terms, so the FFmpeg fallback can catch it
			return nil, fmt.Errorf("failed to decode image: %w", err)
		}
		return img, nil
	}
	img, _, err = image.Decode(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	return img, nil
}

// countWebPFrames returns the number of animation frames (ANMF chunks) in a WebP file.
func countWebPFrames(data []byte) int {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0
	}
	frames := 0
	for i := 12; i+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if string(data[i:i+4]) == "ANMF" {
			frames++
		}
		i += 8 + size + size&1
	}
	return frames
}
