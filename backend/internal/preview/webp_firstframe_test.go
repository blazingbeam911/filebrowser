package preview

// fufu: tests for decodeFirstWebPFrame (webp_decode.go), the first-frame decode of animated
// WebP used by decodePreviewImage (image_decode.go).

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"

	dwebp "github.com/deepteams/webp"
	"github.com/deepteams/webp/animation"
	"github.com/kovidgoyal/imaging"
)

// animatedWebPTest encodes an animated lossless WebP from the given frames.
func animatedWebPTest(t *testing.T, w, h int, frames []image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, w, h, &animation.EncodeOptions{Lossless: true})
	for _, f := range frames {
		if err := enc.AddFrame(f, 40*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// solidFramesTest returns n w×h frames, each one solid colour (cycling through 4).
func solidFramesTest(w, h, n int) []image.Image {
	colors := []color.NRGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 0, 255}}
	var frames []image.Image
	for i := 0; i < n; i++ {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.Draw(img, img.Bounds(), image.NewUniform(colors[i%len(colors)]), image.Point{}, draw.Src)
		frames = append(frames, img)
	}
	return frames
}

// A preview of an animated WebP decodes only its first frame. imaging would decode all
// 120 frames (about 170 MB here).
func TestPreviewOfAnimatedWebPDecodesOneFrame(t *testing.T) {
	const w, h, frames = 600, 600, 120
	data := animatedWebPTest(t, w, h, solidFramesTest(w, h, frames))
	if n := countWebPFrames(data); n != frames {
		t.Fatalf("test file has %d frames, want %d", n, frames)
	}
	svc := NewPreviewGenerator(1, t.TempDir())
	var out bytes.Buffer
	var err error
	n := allocatedBy(func() {
		err = svc.Resize(context.Background(), bytes.NewReader(data), &out,
			ResizeOptions{Width: 512, Height: 512, Format: FormatWebp})
	})
	if err != nil {
		t.Fatalf("Resize: %v", err)
	}
	t.Logf("allocated %d KB for a preview of %d frames", n>>10, frames)
	if n > 40<<20 {
		t.Fatalf("allocated %d MB for a preview, want under 40 MB (all frames decoded?)", n>>20)
	}
	// Thumbnail jobs decode through decodeForPreview (pregenerate.go).
	var img image.Image
	n = allocatedBy(func() { img, err = decodeForPreview(bytes.NewReader(data), FormatWebp) })
	if err != nil {
		t.Fatal(err)
	}
	if n > 40<<20 {
		t.Fatalf("decodeForPreview allocated %d MB, want under 40 MB (all frames decoded?)", n>>20)
	}
	if got, want := color.NRGBAModel.Convert(img.At(w/2, h/2)), (color.NRGBA{255, 0, 0, 255}); got != want {
		t.Fatalf("pixel = %v, want the first frame's %v", got, want)
	}
}

// An animated WebP over the frame limit (canvas × frames) still gets a preview, from its
// first frame.
func TestPreviewOfLongAnimatedWebP(t *testing.T) {
	const w, h, frames = 1000, 1000, 201 // 201 million pixels over all frames
	data := animatedWebPTest(t, w, h, solidFramesTest(w, h, frames))
	svc := NewPreviewGenerator(1, t.TempDir())
	var out bytes.Buffer
	if err := svc.Resize(context.Background(), bytes.NewReader(data), &out,
		ResizeOptions{Width: 512, Height: 512, Format: FormatWebp}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if out.Len() < minPreviewSize {
		t.Fatalf("preview is %d bytes", out.Len())
	}
}

// offsetFrameWebPTest builds an animated WebP with a cw×ch canvas whose frames are frame,
// first at (x, y) (even numbers: the format stores half the offset), then at 0,0. Built by
// hand, as the encoder always stores the first frame at full canvas size.
func offsetFrameWebPTest(t *testing.T, cw, ch, x, y int, frame image.Image) []byte {
	t.Helper()
	var still bytes.Buffer
	if err := dwebp.Encode(&still, frame, &dwebp.EncoderOptions{Lossless: true}); err != nil {
		t.Fatal(err)
	}
	if string(still.Bytes()[12:16]) != "VP8L" {
		t.Fatalf("still WebP starts with chunk %q, want VP8L", still.Bytes()[12:16])
	}
	vp8l := still.Bytes()[12:] // the VP8L chunk
	le24 := func(b []byte, v int) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }
	chunk := func(fourcc string, payload []byte) []byte {
		c := append([]byte(fourcc), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(c[4:], uint32(len(payload)))
		c = append(c, payload...)
		if len(payload)%2 == 1 {
			c = append(c, 0)
		}
		return c
	}
	anmf := func(fx, fy int) []byte {
		h := make([]byte, 16)
		le24(h[0:], fx/2)
		le24(h[3:], fy/2)
		le24(h[6:], frame.Bounds().Dx()-1)
		le24(h[9:], frame.Bounds().Dy()-1)
		le24(h[12:], 40) // duration, ms
		return chunk("ANMF", append(h, vp8l...))
	}
	vp8x := make([]byte, 10)
	vp8x[0] = 0x02 | 0x10 // animation, alpha
	le24(vp8x[4:], cw-1)
	le24(vp8x[7:], ch-1)
	body := append([]byte("WEBP"), chunk("VP8X", vp8x)...)
	body = append(body, chunk("ANIM", make([]byte, 6))...)
	body = append(body, anmf(x, y)...)
	body = append(body, anmf(0, 0)...)
	riff := append([]byte("RIFF"), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(riff[4:], uint32(len(body)))
	return append(riff, body...)
}

// A first frame smaller than the canvas is placed at its offset, as imaging does.
func TestFirstWebPFrameOnCanvas(t *testing.T) {
	const w, h, fx, fy = 200, 150, 60, 40
	frame := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	for i := range frame.Pix {
		frame.Pix[i] = uint8(i * 7)
		if i%4 == 3 {
			frame.Pix[i] = 255
		}
	}
	data := offsetFrameWebPTest(t, w, h, fx, fy, frame)
	if x, y := firstWebPFrameOffset(data); x != fx || y != fy {
		t.Fatalf("firstWebPFrameOffset = %d,%d, want %d,%d", x, y, fx, fy)
	}
	got, err := decodePreviewImage(bytes.NewReader(data), FormatWebp)
	if err != nil {
		t.Fatal(err)
	}
	want, err := imaging.Decode(bytes.NewReader(data), previewDecodeOpts(true)...)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != want.Bounds() {
		t.Fatalf("bounds %v, imaging gives %v", got.Bounds(), want.Bounds())
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			g, wt := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA), color.NRGBAModel.Convert(want.At(x, y)).(color.NRGBA)
			if g.A == 0 && wt.A == 0 {
				continue // fully transparent: colour doesn't matter
			}
			if g != wt {
				t.Fatalf("pixel %d,%d = %v, imaging gives %v", x, y, g, wt)
			}
		}
	}
	if c := color.NRGBAModel.Convert(got.At(fx+1, fy+1)).(color.NRGBA); c.A != 255 {
		t.Fatalf("pixel inside the frame is %v, want opaque", c)
	}
	if c := color.NRGBAModel.Convert(got.At(fx-1, fy-1)).(color.NRGBA); c.A != 0 {
		t.Fatalf("pixel outside the frame is %v, want transparent", c)
	}
}
