package preview

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// psdSpec describes a PSD built by buildPSD.
type psdSpec struct {
	psb         bool
	w, h, ch    int
	depth, mode uint16
	layers      int  // layer count written into the layer info
	noLayerInfo bool // write an empty layer and mask section
	comp        int  // 0 raw, 1 RLE, other values written as-is
	alphaIDs    []uint32
	planes      [][]byte // per channel, w*h bytes; generated when nil
	layerPad    int      // extra bytes inside the layer section (a "big" layered file)
}

func (s *psdSpec) defaults() {
	if s.depth == 0 {
		s.depth = 8
	}
	if s.mode == 0 {
		s.mode = 3
	}
	if s.planes == nil {
		s.planes = make([][]byte, s.ch)
		for c := range s.planes {
			s.planes[c] = make([]byte, s.w*s.h)
			for i := range s.planes[c] {
				// Runs of equal bytes and changing bytes, different per channel.
				s.planes[c][i] = byte((i/3)*7 + c*50)
			}
		}
	}
}

// packBits encodes one row the way Photoshop does (repeats of 3+ bytes, literals otherwise).
func packBits(row []byte) []byte {
	var out []byte
	for i := 0; i < len(row); {
		j := i + 1
		for j < len(row) && j-i < 128 && row[j] == row[i] {
			j++
		}
		if j-i >= 3 {
			out = append(out, byte(int8(1-(j-i))), row[i])
			i = j
			continue
		}
		start := i
		for i < len(row) && i-start < 128 {
			if i+2 < len(row) && row[i] == row[i+1] && row[i] == row[i+2] {
				break
			}
			i++
		}
		out = append(out, byte(i-start-1))
		out = append(out, row[start:i]...)
	}
	return out
}

func buildPSD(s psdSpec) []byte {
	s.defaults()
	var b bytes.Buffer
	be := binary.BigEndian
	put16 := func(v uint16) { _ = binary.Write(&b, be, v) }
	put32 := func(v uint32) { _ = binary.Write(&b, be, v) }
	putLen := func(v uint64) {
		if s.psb {
			_ = binary.Write(&b, be, v)
		} else {
			put32(uint32(v))
		}
	}
	b.WriteString("8BPS")
	if s.psb {
		put16(2)
	} else {
		put16(1)
	}
	b.Write(make([]byte, 6))
	put16(uint16(s.ch))
	put32(uint32(s.h))
	put32(uint32(s.w))
	put16(s.depth)
	put16(s.mode)
	put32(0) // color mode data
	// Image resources: optional 1053 (alpha identifiers), behind an unrelated resource.
	var res bytes.Buffer
	res.WriteString("8BIM")
	_ = binary.Write(&res, be, uint16(1005))
	res.Write([]byte{0, 0}) // empty name, padded
	_ = binary.Write(&res, be, uint32(3))
	res.Write([]byte{1, 2, 3, 0}) // odd length, padded
	if s.alphaIDs != nil {
		res.WriteString("8BIM")
		_ = binary.Write(&res, be, uint16(1053))
		res.Write([]byte{0, 0})
		_ = binary.Write(&res, be, uint32(4*len(s.alphaIDs)))
		for _, id := range s.alphaIDs {
			_ = binary.Write(&res, be, id)
		}
	}
	put32(uint32(res.Len()))
	b.Write(res.Bytes())
	// Layer and mask section.
	if s.noLayerInfo {
		putLen(0)
	} else {
		lenSize := uint64(4)
		if s.psb {
			lenSize = 8
		}
		// layer info = count + some opaque bytes the reader must skip.
		junk := make([]byte, 6+s.layerPad)
		putLen(lenSize + 2 + uint64(len(junk)) + 4)
		putLen(2 + uint64(len(junk)))
		put16(uint16(int16(s.layers)))
		b.Write(junk)
		put32(0) // global layer mask info length
	}
	put16(uint16(s.comp))
	switch s.comp {
	case 0:
		for _, p := range s.planes {
			b.Write(p)
		}
	case 1:
		var rows [][]byte
		for _, p := range s.planes {
			for y := 0; y < s.h; y++ {
				rows = append(rows, packBits(p[y*s.w:(y+1)*s.w]))
			}
		}
		for _, r := range rows {
			if s.psb {
				put32(uint32(len(r)))
			} else {
				put16(uint16(len(r)))
			}
		}
		for _, r := range rows {
			b.Write(r)
		}
	}
	return b.Bytes()
}

func decodeBytes(t *testing.T, data []byte, k int) (*image.NRGBA, psdInfo, error) {
	t.Helper()
	r := bytes.NewReader(data)
	info, err := readPSDInfo(r, int64(len(data)))
	if err != nil {
		return nil, info, err
	}
	img, err := decodePSD(context.Background(), r, info, k)
	return img, info, err
}

// wantPixel is pixel (x, y) at k = 1.
func wantPixel(s psdSpec, alpha bool, x, y int) [4]byte {
	i := y*s.w + x
	px := [4]byte{s.planes[0][i], s.planes[1][i], s.planes[2][i], 255}
	if alpha {
		px[3] = s.planes[3][i]
		if px[3] == 0 {
			return [4]byte{}
		}
	}
	return px
}

func checkExact(t *testing.T, s psdSpec, alpha bool) {
	t.Helper()
	s.defaults()
	img, info, err := decodeBytes(t, buildPSD(s), 1)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.alpha != alpha {
		t.Fatalf("alpha = %v, want %v", info.alpha, alpha)
	}
	if img.Bounds().Dx() != s.w || img.Bounds().Dy() != s.h {
		t.Fatalf("size %v, want %dx%d", img.Bounds(), s.w, s.h)
	}
	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			o := img.PixOffset(x, y)
			got := [4]byte(img.Pix[o : o+4])
			if want := wantPixel(s, alpha, x, y); got != want {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

func TestPSDExactPixels(t *testing.T) {
	cases := []struct {
		name  string
		spec  psdSpec
		alpha bool
	}{
		{"raw RGB", psdSpec{w: 5, h: 3, ch: 3, layers: 2, comp: 0}, false},
		{"RLE RGB odd width", psdSpec{w: 7, h: 5, ch: 3, layers: 1, comp: 1}, false},
		{"RLE RGB wide rows (runs over 128)", psdSpec{w: 300, h: 4, ch: 3, layers: 1, comp: 1}, false},
		{"RLE solid rows (repeat runs of 128)", psdSpec{w: 300, h: 2, ch: 3, layers: 1, comp: 1, planes: [][]byte{
			bytes.Repeat([]byte{7}, 600), bytes.Repeat([]byte{8}, 600), bytes.Repeat([]byte{9}, 600)}}, false},
		{"RLE negative layer count = transparency", psdSpec{w: 9, h: 4, ch: 4, layers: -3, comp: 1}, true},
		{"RLE positive layer count = no transparency", psdSpec{w: 9, h: 4, ch: 4, layers: 3, comp: 1}, false},
		{"12 channels, positive count", psdSpec{w: 6, h: 3, ch: 12, layers: 36, comp: 1}, false},
		{"5 channels, negative count", psdSpec{w: 6, h: 3, ch: 5, layers: -75, comp: 0}, true},
		{"no layers, 4 channels = transparency", psdSpec{w: 4, h: 4, ch: 4, noLayerInfo: true, comp: 1}, true},
		{"no layers, selections only", psdSpec{w: 4, h: 4, ch: 4, noLayerInfo: true, comp: 1, alphaIDs: []uint32{7}}, false},
		{"no layers, alpha id 0 = transparency", psdSpec{w: 4, h: 4, ch: 5, noLayerInfo: true, comp: 1, alphaIDs: []uint32{0, 7}}, true},
		{"PSB RLE", psdSpec{psb: true, w: 11, h: 3, ch: 3, layers: 31, comp: 1}, false},
		{"PSB raw with transparency", psdSpec{psb: true, w: 3, h: 3, ch: 4, layers: -1, comp: 0}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkExact(t, c.spec, c.alpha) })
	}
}

func TestPSDBlockAverage(t *testing.T) {
	// 3×3 image, k = 2: cells of 2×2, 1×2, 2×1 and 1×1 pixels.
	s := psdSpec{w: 3, h: 3, ch: 4, layers: -1, comp: 1, planes: [][]byte{
		{200, 100, 10, 0, 50, 20, 30, 30, 90},     // R
		{0, 0, 0, 0, 0, 0, 0, 0, 0},               // G
		{10, 10, 10, 10, 10, 10, 10, 10, 10},      // B
		{255, 255, 255, 0, 255, 255, 255, 0, 128}, // A
	}}
	img, _, err := decodeBytes(t, buildPSD(s), 2)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
		t.Fatalf("size %v, want 2x2", img.Bounds())
	}
	want := map[[2]int][4]byte{
		// top-left: R 200,100,0(a0),50 weighted by a: (200+100+50)/3 ≈ 117; A 765/4 ≈ 191
		{0, 0}: {117, 0, 10, 191},
		// top-right: R 10 (a255), 20 (a255) → 15; A 255
		{1, 0}: {15, 0, 10, 255},
		// bottom-left: R 30 (a255), 30 (a0) → 30; A 255/2 ≈ 128
		{0, 1}: {30, 0, 10, 128},
		// bottom-right: R 90, A 128
		{1, 1}: {90, 0, 10, 128},
	}
	for xy, w := range want {
		o := img.PixOffset(xy[0], xy[1])
		if got := [4]byte(img.Pix[o : o+4]); got != w {
			t.Errorf("cell %v = %v, want %v", xy, got, w)
		}
	}
}

func TestPSDBlock(t *testing.T) {
	cases := []struct {
		w, h, need int
		cover      bool
		want       int
	}{
		{10000, 6000, 4096, false, 2}, // 2.44 → 2: 5000×3000, then fitted to 4096
		{8000, 5000, 4096, false, 1},  // 1.95 → 1
		{3000, 4000, 4096, false, 1},  // already fits
		{10000, 6000, 1024, true, 5},  // cover: min(9.77, 5.86) → 5
		{1048, 1700, 1024, true, 1},
		{4576, 22559, 16383, false, 1}, // 1.38 → 1, then fitted
	}
	for _, c := range cases {
		if got := psdBlock(c.w, c.h, c.need, c.cover); got != c.want {
			t.Errorf("psdBlock(%d,%d,%d,%v) = %d, want %d", c.w, c.h, c.need, c.cover, got, c.want)
		}
	}
}

func TestPSDUnsupportedAndDamaged(t *testing.T) {
	good := psdSpec{w: 20, h: 6, ch: 3, layers: 1, comp: 1}
	unsupported := []psdSpec{
		{w: 4, h: 4, ch: 3, depth: 16, layers: 1, comp: 0, planes: [][]byte{{}, {}, {}}},
		{w: 4, h: 4, ch: 4, mode: 4, layers: 1, comp: 0}, // CMYK
		{w: 4, h: 4, ch: 3, layers: 1, comp: 2},          // ZIP
	}
	for i, s := range unsupported {
		_, _, err := decodeBytes(t, buildPSD(s), 1)
		if !errors.Is(err, ErrUnsupportedFormat) {
			t.Errorf("unsupported case %d: err = %v, want ErrUnsupportedFormat", i, err)
		}
	}
	// Header claiming more pixels than the preview limit: refused before reading data.
	huge := buildPSD(psdSpec{w: 2, h: 2, ch: 3, layers: 1, comp: 0})
	binary.BigEndian.PutUint32(huge[14:], 20000)
	binary.BigEndian.PutUint32(huge[18:], 20000)
	if _, _, err := decodeBytes(t, huge, 1); !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("huge header: err = %v, want ErrImageTooLarge", err)
	}
	// Every truncation of a valid file is an error, never a panic or a partial picture.
	full := buildPSD(good)
	for n := 0; n < len(full); n++ {
		if _, _, err := decodeBytes(t, full[:n], 1); err == nil {
			t.Fatalf("truncated to %d of %d bytes: no error", n, len(full))
		}
	}
	// Layer section longer than the file (the truncated real files).
	long := buildPSD(good)
	off := 26 + 4
	resLen := int(binary.BigEndian.Uint32(long[off:]))
	lmOff := off + 4 + resLen
	binary.BigEndian.PutUint32(long[lmOff:], 72054674)
	if _, _, err := decodeBytes(t, long, 1); !errors.Is(err, errPSDDamaged) {
		t.Errorf("long layer section: err = %v, want errPSDDamaged", err)
	}
	// Corrupt PackBits: a literal run longer than the row.
	bad := buildPSD(good)
	info, err := readPSDInfo(bytes.NewReader(bad), int64(len(bad)))
	if err != nil {
		t.Fatal(err)
	}
	firstRow := info.dataStart + int64(good.ch*good.h*2)
	bad[firstRow] = 127 // literal of 128 bytes in a 20-pixel row
	if _, _, err := decodeBytes(t, bad, 1); !errors.Is(err, errPSDDamaged) {
		t.Errorf("corrupt PackBits: err = %v, want errPSDDamaged", err)
	}
	// Row table claiming rows too short to hold a row.
	short := buildPSD(good)
	binary.BigEndian.PutUint16(short[info.dataStart:], 1)
	if _, _, err := decodeBytes(t, short, 1); !errors.Is(err, errPSDDamaged) {
		t.Errorf("short RLE row: err = %v, want errPSDDamaged", err)
	}
}

func TestIsPSD(t *testing.T) {
	if !IsPSD(PSDMimeType, "x.bin") || !IsPSD("", "A.PSD") || !IsPSD("", "b.psb") || IsPSD("image/png", "c.png") {
		t.Fatal("IsPSD")
	}
}

func FuzzReadPSD(f *testing.F) {
	f.Add(buildPSD(psdSpec{w: 7, h: 5, ch: 3, layers: 1, comp: 1}))
	f.Add(buildPSD(psdSpec{w: 5, h: 3, ch: 4, layers: -1, comp: 0}))
	f.Add(buildPSD(psdSpec{psb: true, w: 11, h: 3, ch: 3, layers: 31, comp: 1}))
	// Found by fuzzing: a PSB header whose width × height overflowed int64 past the pixel limit,
	// then allocated gigabytes of row buffers (fixed by checking the format's side limits first).
	f.Add([]byte("8BPS\x00\x02\x00\x00\x00\x03\x00~Cշ\xcdE\x96\xef\x00\x00\v\x00\b\x00\x03\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\b\x00\x1f\x01\xfej\xfeq\xfex\xfe\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		defer func() {
			runtime.ReadMemStats(&m1)
			if d := m1.TotalAlloc - m0.TotalAlloc; d > 64<<20 {
				t.Fatalf("allocated %d MB for a %d-byte input", d>>20, len(data))
			}
		}()
		r := bytes.NewReader(data)
		info, err := readPSDInfo(r, int64(len(data)))
		if err != nil {
			return
		}
		_, _ = decodePSD(context.Background(), r, info, psdBlock(info.width, info.height, 64, false))
	})
}

// TestPSDRealSamples compares the reader with psd-tools references, pixel for pixel, on real
// files. It runs only when both directories are given:
//
//	FBQ_PSD_SAMPLES=~/fbq-test/samples/psd-fetch/full FBQ_PSD_REFS=~/fbq-test/samples/psd-ref
func TestPSDRealSamples(t *testing.T) {
	samples, refs := os.Getenv("FBQ_PSD_SAMPLES"), os.Getenv("FBQ_PSD_REFS")
	if samples == "" || refs == "" {
		t.Skip("FBQ_PSD_SAMPLES / FBQ_PSD_REFS not set")
	}
	entries, err := os.ReadDir(samples)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			f, info, err := openPSD(filepath.Join(samples, name))
			refPath := filepath.Join(refs, strings.TrimSuffix(name, filepath.Ext(name))+".png")
			if _, statErr := os.Stat(refPath); statErr != nil {
				if err == nil {
					t.Fatalf("no reference, but the reader opened it")
				}
				t.Logf("no reference; reader error (expected for damaged files): %v", err)
				return
			}
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer f.Close()
			got, err := decodePSD(context.Background(), f, info, 1)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			rf, err := os.Open(refPath)
			if err != nil {
				t.Fatal(err)
			}
			defer rf.Close()
			ref, err := png.Decode(rf)
			if err != nil {
				t.Fatal(err)
			}
			if ref.Bounds() != got.Bounds() {
				t.Fatalf("size %v, reference %v", got.Bounds(), ref.Bounds())
			}
			diff := 0
			b := got.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					o := got.PixOffset(x, y)
					g := got.Pix[o : o+4]
					r16, g16, b16, a16 := ref.At(x, y).RGBA()
					ra := uint8(a16 >> 8)
					if g[3] != ra {
						diff++
						continue
					}
					if ra == 0 {
						continue
					}
					// Reference colors are premultiplied by At(); compare at full alpha only.
					if ra == 255 && (g[0] != uint8(r16>>8) || g[1] != uint8(g16>>8) || g[2] != uint8(b16>>8)) {
						diff++
					}
				}
			}
			t.Logf("%dx%d alpha=%v compression=%d psb=%v: %d differing pixels", b.Dx(), b.Dy(), info.alpha, info.compression, info.psb, diff)
			if diff != 0 {
				t.Fatalf("%d pixels differ from the reference", diff)
			}
		})
	}
}
