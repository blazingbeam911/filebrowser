package preview

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
	"github.com/kovidgoyal/imaging"
	"github.com/kovidgoyal/imaging/apng"
)

// decodeTestPalette has one colour per frame index (mod 4), so each frame differs.
var decodeTestPalette = color.Palette{
	color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 255, 0, 255},
	color.NRGBA{0, 0, 255, 255}, color.NRGBA{255, 255, 0, 255},
}

// decodeTestFrame is a w×h frame filled with palette colour i%4.
func decodeTestFrame(w, h, i int) *image.Paletted {
	img := image.NewPaletted(image.Rect(0, 0, w, h), decodeTestPalette)
	for p := range img.Pix {
		img.Pix[p] = uint8(i % len(decodeTestPalette))
	}
	return img
}

func decodeTestGIF(t *testing.T, w, h, frames int) []byte {
	t.Helper()
	g := &gif.GIF{}
	for i := 0; i < frames; i++ {
		g.Image = append(g.Image, decodeTestFrame(w, h, i))
		g.Delay = append(g.Delay, 4)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeTestAPNG(t *testing.T, w, h, frames int) []byte {
	t.Helper()
	a := apng.APNG{}
	for i := 0; i < frames; i++ {
		a.Frames = append(a.Frames, apng.Frame{Image: decodeTestFrame(w, h, i), DelayNumerator: 1, DelayDenominator: 25})
	}
	var buf bytes.Buffer
	if err := apng.Encode(&buf, a); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// decodeTestHugePNG is a valid PNG header (IHDR) claiming w×h, without image data.
func decodeTestHugePNG(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()[:33] // signature + IHDR chunk
	binary.BigEndian.PutUint32(data[16:20], w)
	binary.BigEndian.PutUint32(data[20:24], h)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return append(data, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82)
}

// decodeTestHugeJPEG is a small JPEG whose frame header claims w×h, padded to size bytes.
func decodeTestHugeJPEG(t *testing.T, w, h uint16, size int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	sof := bytes.Index(data, []byte{0xff, 0xc0})
	if sof < 0 {
		t.Fatal("no SOF0 marker")
	}
	binary.BigEndian.PutUint16(data[sof+5:], h)
	binary.BigEndian.PutUint16(data[sof+7:], w)
	if len(data) < size {
		data = append(data, make([]byte, size-len(data))...)
	}
	return data
}

// decodeTestAnimatedWebP is the RIFF structure of an animated WebP with a w×h canvas and
// the given number of frame chunks. The frames hold no image data: only the header and the
// frame count are read before a refusal.
func decodeTestAnimatedWebP(w, h, frames int) []byte {
	le24 := func(b []byte, v int) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }
	chunk := func(fourcc string, payload []byte) []byte {
		c := append([]byte(fourcc), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(c[4:], uint32(len(payload)))
		return append(c, payload...)
	}
	vp8x := make([]byte, 10)
	vp8x[0] = 0x02 // animation
	le24(vp8x[4:], w-1)
	le24(vp8x[7:], h-1)
	body := append([]byte("WEBP"), chunk("VP8X", vp8x)...)
	body = append(body, chunk("ANIM", make([]byte, 6))...)
	for i := 0; i < frames; i++ {
		anmf := make([]byte, 16)
		le24(anmf[6:], w-1)
		le24(anmf[9:], h-1)
		body = append(body, chunk("ANMF", anmf)...)
	}
	riff := append([]byte("RIFF"), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(riff[4:], uint32(len(body)))
	return append(riff, body...)
}

// allocatedBy returns the bytes allocated while f runs.
func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// A preview of an animated GIF or PNG decodes only one frame: memory stays near one
// frame's size however many frames the file has.
func TestPreviewOfAnimationDecodesOneFrame(t *testing.T) {
	const w, h, frames = 1000, 1000, 120 // 1 MB per frame, 120 MB for all of them
	const limit = 40 << 20
	svc := NewPreviewGenerator(1, t.TempDir())
	for _, tc := range []struct {
		name   string
		data   []byte
		format Format
	}{
		{"gif", decodeTestGIF(t, w, h, frames), FormatGif},
		{"apng", decodeTestAPNG(t, w, h, frames), FormatPng},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			var err error
			n := allocatedBy(func() {
				err = svc.Resize(context.Background(), bytes.NewReader(tc.data), &out,
					ResizeOptions{Width: 512, Height: 512, Format: tc.format})
			})
			if err != nil {
				t.Fatalf("Resize: %v", err)
			}
			t.Logf("allocated %d KB for a preview of %d frames", n>>10, frames)
			if n > limit {
				t.Fatalf("allocated %d MB for a preview, want under %d MB (all frames decoded?)", n>>20, limit>>20)
			}
			img, err := decodePreviewImage(bytes.NewReader(tc.data), tc.format)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := color.NRGBAModel.Convert(img.At(w/2, h/2)), decodeTestPalette[0]; got != want {
				t.Fatalf("pixel = %v, want the first frame's %v", got, want)
			}
		})
	}
}

// Images larger than maxPreviewPixels are refused from their header, without decoding.
func TestPreviewRefusesTooManyPixels(t *testing.T) {
	svc := NewPreviewGenerator(1, t.TempDir())
	for _, tc := range []struct {
		name   string
		data   []byte
		format Format
	}{
		{"png", decodeTestHugePNG(t, 15000, 14000), FormatPng},
		{"jpeg", decodeTestHugeJPEG(t, 15000, 14000, 0), FormatJpeg},
		// 1000×1000 canvas × 201 frames = 201 million pixels
		{"animated webp", decodeTestAnimatedWebP(1000, 1000, 201), FormatWebp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := svc.Resize(context.Background(), bytes.NewReader(tc.data), &out,
				ResizeOptions{Width: 512, Height: 512, Format: tc.format})
			if !errors.Is(err, ErrImageTooLarge) {
				t.Fatalf("Resize err = %v, want ErrImageTooLarge", err)
			}
		})
	}
}

// An animated WebP within the limit isn't refused (it is decoded as before).
func TestPreviewKeepsShortAnimatedWebP(t *testing.T) {
	// 1000×1000 × 200 frames = exactly the limit; the test file has no image data, so
	// decoding fails, but not with ErrImageTooLarge.
	_, err := decodePreviewImage(bytes.NewReader(decodeTestAnimatedWebP(1000, 1000, 200)), FormatWebp)
	if err == nil || errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("err = %v, want a decode error other than ErrImageTooLarge", err)
	}
}

func TestCountWebPFrames(t *testing.T) {
	for _, n := range []int{0, 1, 7} {
		if got := countWebPFrames(decodeTestAnimatedWebP(10, 10, n)); got != n {
			t.Errorf("countWebPFrames(%d frames) = %d", n, got)
		}
	}
	if got := countWebPFrames([]byte("not a webp file")); got != 0 {
		t.Errorf("countWebPFrames(garbage) = %d, want 0", got)
	}
	// A truncated file: a last frame whose chunk header is complete still counts (erring
	// towards more frames is the safe side for the limit); one cut inside its header doesn't.
	full := decodeTestAnimatedWebP(10, 10, 3) // each ANMF chunk is 8 + 16 bytes
	if got := countWebPFrames(full[:len(full)-5]); got != 3 {
		t.Errorf("countWebPFrames(cut in last frame's data) = %d, want 3", got)
	}
	if got := countWebPFrames(full[:len(full)-20]); got != 2 {
		t.Errorf("countWebPFrames(cut in last frame's header) = %d, want 2", got)
	}
}

// A JPEG that is too large gets no preview and doesn't go to the FFmpeg fallback.
func TestGenerateImagePreviewTooLargeSkipsFallback(t *testing.T) {
	dir := t.TempDir()
	data := decodeTestHugeJPEG(t, 15000, 14000, 256*1024+1) // over the serve-the-original size
	path := filepath.Join(dir, "huge.jpg")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	svc := NewPreviewGenerator(1, dir)
	file := iteminfo.ExtendedFileInfo{
		FileInfo: iteminfo.FileInfo{
			ItemInfo: iteminfo.ItemInfo{
				Name:    "huge.jpg",
				Size:    int64(len(data)),
				ModTime: time.Now(),
				Type:    "image/jpeg",
			},
		},
		RealPath: path,
	}
	_, err := svc.generateImagePreview(context.Background(), file, "large")
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("generateImagePreview err = %v, want ErrImageTooLarge", err)
	}
}

// Images within the limit decode to exactly what imaging.Decode gives, including GIFs
// (first frame, origin moved to 0,0) that now go through image/gif.
func TestDecodePreviewImageMatchesImaging(t *testing.T) {
	var jpg, pngData, offsetGIF bytes.Buffer
	src := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 13)
	}
	if err := jpeg.Encode(&jpg, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pngData, src); err != nil {
		t.Fatal(err)
	}
	// A GIF whose only frame is smaller than the canvas and offset within it.
	frame := decodeTestFrame(20, 10, 2)
	frame.Rect = image.Rect(5, 7, 25, 17)
	if err := gif.EncodeAll(&offsetGIF, &gif.GIF{
		Image: []*image.Paletted{frame}, Delay: []int{0},
		Config: image.Config{ColorModel: decodeTestPalette, Width: 40, Height: 30},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		data   []byte
		format Format
	}{
		{"jpeg", jpg.Bytes(), FormatJpeg},
		{"png", pngData.Bytes(), FormatPng},
		{"gif with offset frame", offsetGIF.Bytes(), FormatGif},
		{"animated gif", decodeTestGIF(t, 30, 20, 5), FormatGif},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := imaging.Decode(bytes.NewReader(tc.data), previewDecodeOpts(true)...)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodePreviewImage(bytes.NewReader(tc.data), tc.format)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decoded %T %v, imaging gives %T %v", got, got.Bounds(), want, want.Bounds())
			}
		})
	}
}
