package preview

import (
	"bytes"
	"image"
	"testing"

	"github.com/gen2brain/webp"
	xwebp "golang.org/x/image/webp"
)

// TestWebPDecoderIsDeepteams pins the WebP decoder: image.Decode (and so imaging.Decode for
// still WebP) must use deepteams/webp, and the encoder's copy must not register gen2brain's.
// A lossy WebP decodes to *image.NRGBA with deepteams and to *image.YCbCr with x/image. Previews
// of animated WebP also rely on it: deepteams' registered decoder returns an animated file's
// first frame (decodeFirstWebPFrame), where x/image can't read the file at all.
func TestWebPDecoderIsDeepteams(t *testing.T) {
	if d := activeWebPDecoder(); d != "deepteams/webp" {
		t.Fatalf("active WebP decoder is %q, want deepteams/webp", d)
	}

	// Lossless: identical pixels to x/image (exact decode either way).
	src := noisyImage(64, 48, true)
	var lossless bytes.Buffer
	if err := webp.Encode(&lossless, src, webp.Options{Lossless: true, Exact: true}); err != nil {
		t.Fatal(err)
	}
	got, format, err := image.Decode(bytes.NewReader(lossless.Bytes()))
	if err != nil || format != "webp" {
		t.Fatalf("lossless: format %q, err %v", format, err)
	}
	ref, err := xwebp.Decode(bytes.NewReader(lossless.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	g, ok := got.(*image.NRGBA)
	if !ok {
		t.Fatalf("lossless decoded as %T, want *image.NRGBA", got)
	}
	r := ref.(*image.NRGBA)
	if !bytes.Equal(g.Pix, r.Pix) {
		t.Fatal("lossless WebP: deepteams and x/image pixels differ")
	}

	// DecodeConfig goes through the same registration.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(lossless.Bytes()))
	if err != nil || format != "webp" || cfg.Width != 64 || cfg.Height != 48 {
		t.Fatalf("DecodeConfig: %+v %q %v", cfg, format, err)
	}
}

// TestWebPDecodeOfDamagedFilesIsSafe: truncated or corrupted WebP must give an error (or an
// image), never a panic, through the registered decoder.
func TestWebPDecodeOfDamagedFilesIsSafe(t *testing.T) {
	var buf bytes.Buffer
	if err := webp.Encode(&buf, noisyImage(200, 150, false), webp.Options{Quality: 75}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	for cut := 12; cut < len(b); cut += len(b) / 40 {
		_, _, _ = image.Decode(bytes.NewReader(b[:cut]))
	}
	for k := 20; k < len(b); k += 37 {
		bad := append([]byte(nil), b...)
		bad[k] ^= 0x5a
		_, _, _ = image.Decode(bytes.NewReader(bad))
	}
}

// TestSafeDecodeRecoversPanics: the registration wrappers turn a panic inside the decoder into
// an error. The reader hands out a WebP signature (so image.Decode picks the WebP decoder),
// then panics on the decoder's next read.
func TestSafeDecodeRecoversPanics(t *testing.T) {
	_, _, err := image.Decode(&panicReader{})
	if err == nil {
		t.Fatal("expected an error from a reader that panics inside the decoder")
	}
}

type panicReader struct{ sent bool }

func (p *panicReader) Read(b []byte) (int, error) {
	if !p.sent {
		p.sent = true
		return copy(b, "RIFF\x40\x00\x00\x00WEBPVP8L"), nil
	}
	panic("boom")
}
