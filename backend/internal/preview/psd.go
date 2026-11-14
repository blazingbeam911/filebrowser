package preview

// fufu: previews of Photoshop files (PSD and PSB) from the flattened image every PSD stores
// after its layers. The image library can't decode PSD, and decoders that can (MuPDF, FFmpeg)
// load the whole file, which for a layered PSD can be gigabytes. This reader seeks past the
// layers by their recorded length and decodes the flattened image one row at a time,
// averaging blocks of pixels as it goes, so memory follows the size of the preview, not of
// the file.
//
// Supported: 8-bit RGB, raw or RLE (PackBits) compressed, PSD and PSB. Anything else, and
// damaged files, get no preview.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	"os"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
	"github.com/kovidgoyal/imaging"
)

// PSDMimeType is the type FileBrowser gives .psd and .psb files.
const PSDMimeType = iteminfo.PSDMimeType

const (
	// psdThumbSide: thumbnails (small, large, xlarge) are all made from one image decoded to
	// cover psdThumbSide × psdThumbSide, so browsing and thumbnail jobs produce the same bytes.
	psdThumbSide = 1024
	// psdOriginalSide is the long side of the "original" preview the viewer shows.
	psdOriginalSide = 4096
	// psdFullMaxSide is the most a JPEG image can have on a side.
	psdFullMaxSide = 65535
	// psdFullMaxPixels caps the "full" preview, which is decoded whole before encoding.
	psdFullMaxPixels = 128_000_000
	// psdViewJPEGQuality: the viewer sizes ("original", "full") are JPEG. For images of tens of
	// megapixels the WebP encoder (translated libwebp) needs gigabytes of memory and several
	// seconds, while Go's JPEG encoder needs little more than the image itself.
	psdViewJPEGQuality = 90
	// psdMaxResources is the largest image-resources section read (for resource 1053).
	psdMaxResources = 16 << 20
	psdBufferSize   = 1 << 20
)

// errPSDDamaged means the file's structure is inconsistent (truncated, bad lengths, bad RLE).
var errPSDDamaged = errors.New("damaged PSD file")

// IsPSD reports whether a file is a Photoshop document, by type or extension.
func IsPSD(fileType, name string) bool {
	return fileType == PSDMimeType || iteminfo.IsPSDName(name)
}

// psdInfo is what the reader needs from a PSD's header and sections.
type psdInfo struct {
	psb         bool
	channels    int
	width       int
	height      int
	alpha       bool  // channel 3 of the flattened image is its transparency
	compression int   // 0 raw, 1 RLE
	dataStart   int64 // first byte after the compression field
	size        int64
}

func damaged(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errPSDDamaged}, args...)...)
}

// readAtFull reads len(buf) bytes at off, treating a short read as damage.
func readAtFull(r io.ReaderAt, buf []byte, off int64, what string) error {
	n, err := r.ReadAt(buf, off)
	if n == len(buf) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		return damaged("file ends inside the %s", what)
	}
	return err
}

// readPSDInfo parses the header and finds the flattened image, without reading the layers.
func readPSDInfo(r io.ReaderAt, size int64) (psdInfo, error) {
	var info psdInfo
	info.size = size
	head := make([]byte, 26)
	if err := readAtFull(r, head, 0, "header"); err != nil {
		return info, err
	}
	if string(head[:4]) != "8BPS" {
		return info, damaged("not a PSD file")
	}
	be := binary.BigEndian
	switch be.Uint16(head[4:]) {
	case 1:
	case 2:
		info.psb = true
	default:
		return info, fmt.Errorf("%w: PSD version %d", ErrUnsupportedFormat, be.Uint16(head[4:]))
	}
	info.channels = int(be.Uint16(head[12:]))
	info.height = int(be.Uint32(head[14:]))
	info.width = int(be.Uint32(head[18:]))
	depth := be.Uint16(head[22:])
	mode := be.Uint16(head[24:])
	if depth != 8 || mode != 3 {
		return info, fmt.Errorf("%w: PSD depth %d, color mode %d (only 8-bit RGB)", ErrUnsupportedFormat, depth, mode)
	}
	// The format's own limits (30,000 px per side for PSD, 300,000 for PSB). Checked before
	// any arithmetic with the sizes, which a damaged header could otherwise overflow.
	maxSide := 30000
	if info.psb {
		maxSide = 300000
	}
	if info.channels < 3 || info.channels > 56 || info.width < 1 || info.height < 1 ||
		info.width > maxSide || info.height > maxSide {
		return info, damaged("%d channels, %dx%d", info.channels, info.width, info.height)
	}
	if int64(info.width)*int64(info.height) > maxPreviewPixels {
		return info, errImageTooLarge(uint32(info.width), uint32(info.height), 1)
	}

	off := int64(26)
	u32 := make([]byte, 4)
	// Color mode data (empty for RGB, skipped either way).
	if err := readAtFull(r, u32, off, "color mode data"); err != nil {
		return info, err
	}
	off += 4 + int64(be.Uint32(u32))
	// Image resources: kept only to read the alpha identifiers (1053).
	if err := readAtFull(r, u32, off, "image resources"); err != nil {
		return info, err
	}
	resLen := int64(be.Uint32(u32))
	resStart := off + 4
	off = resStart + resLen
	if off > size {
		return info, damaged("image resources run past the end of the file")
	}
	selectionsOnly := false
	if resLen <= psdMaxResources {
		res := make([]byte, resLen)
		if err := readAtFull(r, res, resStart, "image resources"); err != nil {
			return info, err
		}
		selectionsOnly = psdAlphaIDsAllPositive(res)
	}

	// Layer and mask section: read only its length and the layer count.
	lenSize := int64(4)
	if info.psb {
		lenSize = 8
	}
	lmLen, err := readLength(r, off, lenSize, "layer section")
	if err != nil {
		return info, err
	}
	lmStart := off + lenSize
	layerCount, haveLayers := 0, false
	if lmLen >= uint64(lenSize)+2 {
		liLen, err := readLength(r, lmStart, lenSize, "layer info")
		if err != nil {
			return info, err
		}
		if liLen >= 2 {
			cnt := make([]byte, 2)
			if err := readAtFull(r, cnt, lmStart+lenSize, "layer count"); err != nil {
				return info, err
			}
			layerCount = int(int16(be.Uint16(cnt)))
			haveLayers = true
		}
	}
	if lmLen > uint64(size) {
		return info, damaged("layer section (%d bytes) is longer than the file (%d)", lmLen, size)
	}
	compPos := lmStart + int64(lmLen)

	// Transparency, as Photoshop and psd-tools read it: a negative layer count says channel 3
	// is the flattened image's transparency; a positive one says it isn't; without layers it
	// is, unless the alpha identifiers mark every extra channel as a saved selection.
	if info.channels >= 4 {
		switch {
		case haveLayers && layerCount < 0:
			info.alpha = true
		case haveLayers && layerCount > 0:
			info.alpha = false
		default:
			info.alpha = !selectionsOnly
		}
	}

	comp := make([]byte, 2)
	if err := readAtFull(r, comp, compPos, "flattened image"); err != nil {
		return info, damaged("file ends before the flattened image (layer section %d bytes, file %d)", lmLen, size)
	}
	info.compression = int(be.Uint16(comp))
	if info.compression != 0 && info.compression != 1 {
		return info, fmt.Errorf("%w: PSD flattened image compression %d", ErrUnsupportedFormat, info.compression)
	}
	info.dataStart = compPos + 2
	return info, nil
}

func readLength(r io.ReaderAt, off, size int64, what string) (uint64, error) {
	buf := make([]byte, size)
	if err := readAtFull(r, buf, off, what); err != nil {
		return 0, err
	}
	if size == 8 {
		return binary.BigEndian.Uint64(buf), nil
	}
	return uint64(binary.BigEndian.Uint32(buf)), nil
}

// psdAlphaIDsAllPositive reports whether resource 1053 (alpha identifiers) exists and every
// identifier in it is non-zero, i.e. no extra channel is transparency.
func psdAlphaIDsAllPositive(res []byte) bool {
	be := binary.BigEndian
	for i := 0; i+12 <= len(res) && string(res[i:i+4]) == "8BIM"; {
		id := be.Uint16(res[i+4:])
		nameLen := int(res[i+6])
		j := i + 7 + nameLen
		if nameLen%2 == 0 {
			j++
		}
		if j+4 > len(res) {
			return false
		}
		dataLen := int(be.Uint32(res[j:]))
		data := j + 4
		if dataLen < 0 || data+dataLen > len(res) {
			return false
		}
		if id == 1053 {
			if dataLen == 0 || dataLen%4 != 0 {
				return false
			}
			for k := data; k < data+dataLen; k += 4 {
				if be.Uint32(res[k:]) == 0 {
					return false
				}
			}
			return true
		}
		i = data + dataLen + dataLen%2
	}
	return false
}

// psdRows reads the flattened image's channels one row at a time.
type psdRows struct {
	info    psdInfo
	readers []*bufio.Reader // one per used channel
	rowLens [][]uint32      // RLE: per used channel, compressed length of each row
	packed  []byte
	y       int
}

func newPSDRows(r io.ReaderAt, info psdInfo, used []int) (*psdRows, error) {
	p := &psdRows{info: info, readers: make([]*bufio.Reader, len(used))}
	w, h := int64(info.width), int64(info.height)
	switch info.compression {
	case 0:
		end := info.dataStart + int64(info.channels)*w*h
		if end > info.size {
			return nil, damaged("raw flattened image needs %d bytes, file has %d", end-info.dataStart, info.size-info.dataStart)
		}
		for i, c := range used {
			start := info.dataStart + int64(c)*w*h
			p.readers[i] = bufio.NewReaderSize(io.NewSectionReader(r, start, w*h), psdBufferSize)
		}
	case 1:
		entry := int64(2)
		if info.psb {
			entry = 4
		}
		tableLen := int64(info.channels) * h * entry
		if info.dataStart+tableLen > info.size {
			return nil, damaged("file ends inside the RLE row table")
		}
		table := bufio.NewReaderSize(io.NewSectionReader(r, info.dataStart, tableLen), psdBufferSize)
		starts := make([]int64, info.channels)
		lens := make([]int64, info.channels)
		p.rowLens = make([][]uint32, len(used))
		usedIndex := make(map[int]int, len(used))
		for i, c := range used {
			usedIndex[c] = i
			p.rowLens[i] = make([]uint32, h)
		}
		pos := info.dataStart + tableLen
		buf := make([]byte, entry)
		// PackBits needs at least 2 bytes per 128 pixels, so shorter rows mean damage. This
		// also ties the row buffers allocated later to the file's real size.
		minRow := uint32(2 * ((info.width + 127) / 128))
		for c := 0; c < info.channels; c++ {
			starts[c] = pos
			i, isUsed := usedIndex[c]
			for y := int64(0); y < h; y++ {
				if _, err := io.ReadFull(table, buf); err != nil {
					return nil, damaged("RLE row table: %v", err)
				}
				var n uint32
				if entry == 4 {
					n = binary.BigEndian.Uint32(buf)
				} else {
					n = uint32(binary.BigEndian.Uint16(buf))
				}
				if n < minRow {
					return nil, damaged("RLE row %d of channel %d is %d bytes, at least %d needed", y, c, n, minRow)
				}
				if isUsed {
					p.rowLens[i][y] = n
				}
				pos += int64(n)
			}
			lens[c] = pos - starts[c]
		}
		if pos > info.size {
			return nil, damaged("RLE data needs %d bytes, file has %d", pos-info.dataStart-tableLen, info.size-info.dataStart-tableLen)
		}
		for i, c := range used {
			p.readers[i] = bufio.NewReaderSize(io.NewSectionReader(r, starts[c], lens[c]), psdBufferSize)
		}
		p.packed = make([]byte, 0, 2*info.width+2)
	}
	return p, nil
}

// next reads the next row of every used channel into rows (each info.width bytes).
func (p *psdRows) next(rows [][]byte) error {
	for i, rd := range p.readers {
		if p.info.compression == 0 {
			if _, err := io.ReadFull(rd, rows[i]); err != nil {
				return damaged("row %d: %v", p.y, err)
			}
			continue
		}
		n := int(p.rowLens[i][p.y])
		if cap(p.packed) < n {
			p.packed = make([]byte, n)
		}
		packed := p.packed[:n]
		if _, err := io.ReadFull(rd, packed); err != nil {
			return damaged("row %d: %v", p.y, err)
		}
		if err := unpackBits(packed, rows[i]); err != nil {
			return damaged("row %d: %v", p.y, err)
		}
	}
	p.y++
	return nil
}

// unpackBits decodes one PackBits row into dst, which it must fill exactly.
func unpackBits(src, dst []byte) error {
	o := 0
	for i := 0; i < len(src); {
		n := int(int8(src[i]))
		i++
		switch {
		case n >= 0:
			cnt := n + 1
			if i+cnt > len(src) || o+cnt > len(dst) {
				return errors.New("PackBits literal runs past the row")
			}
			copy(dst[o:], src[i:i+cnt])
			i += cnt
			o += cnt
		case n > -128:
			cnt := 1 - n
			if i >= len(src) || o+cnt > len(dst) {
				return errors.New("PackBits repeat runs past the row")
			}
			b := src[i]
			i++
			for j := 0; j < cnt; j++ {
				dst[o+j] = b
			}
			o += cnt
		}
	}
	if o != len(dst) {
		return fmt.Errorf("PackBits row has %d bytes, want %d", o, len(dst))
	}
	return nil
}

// psdBlock returns the largest block size k (>= 1) for which w×h shrunk by k is still at
// least as large as the final preview: for fit, the image fitted in need×need; for cover
// (cropped thumbnails), an image covering need×need.
func psdBlock(w, h, need int, cover bool) int {
	rw, rh := float64(w)/float64(need), float64(h)/float64(need)
	r := math.Max(rw, rh)
	if cover {
		r = math.Min(rw, rh)
	}
	return max(1, int(math.Floor(r)))
}

// decodePSD decodes the flattened image of the PSD in r, averaging k×k blocks of pixels
// (k >= 1; edge blocks may be partial). Colors are averaged weighted by transparency.
func decodePSD(ctx context.Context, r io.ReaderAt, info psdInfo, k int) (*image.NRGBA, error) {
	used := []int{0, 1, 2}
	if info.alpha {
		used = append(used, 3)
	}
	rows, err := newPSDRows(r, info, used)
	if err != nil {
		return nil, err
	}
	w, h := info.width, info.height
	outW, outH := (w+k-1)/k, (h+k-1)/k
	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	line := make([][]byte, len(used))
	for i := range line {
		line[i] = make([]byte, w)
	}
	sr := make([]uint64, outW)
	sg := make([]uint64, outW)
	sb := make([]uint64, outW)
	sa := make([]uint64, outW)
	inBlock := 0
	for y := 0; y < h; y++ {
		if y%256 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := rows.next(line); err != nil {
			return nil, err
		}
		rr, gg, bb := line[0], line[1], line[2]
		for x := 0; x < w; x++ {
			a := uint64(255)
			if info.alpha {
				a = uint64(line[3][x])
			}
			cx := x / k
			sr[cx] += uint64(rr[x]) * a
			sg[cx] += uint64(gg[x]) * a
			sb[cx] += uint64(bb[x]) * a
			sa[cx] += a
		}
		inBlock++
		if inBlock == k || y == h-1 {
			oy := y / k
			px := out.Pix[oy*out.Stride:]
			for cx := 0; cx < outW; cx++ {
				cw := min(k, w-cx*k)
				count := uint64(cw * inBlock)
				p := px[cx*4 : cx*4+4]
				if a := sa[cx]; a > 0 {
					p[0] = uint8((sr[cx] + a/2) / a)
					p[1] = uint8((sg[cx] + a/2) / a)
					p[2] = uint8((sb[cx] + a/2) / a)
					p[3] = uint8((a + count/2) / count)
				} else {
					p[0], p[1], p[2], p[3] = 0, 0, 0, 0
				}
				sr[cx], sg[cx], sb[cx], sa[cx] = 0, 0, 0, 0
			}
			inBlock = 0
		}
	}
	return out, nil
}

// openPSD opens path and reads its PSD info.
func openPSD(path string) (*os.File, psdInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, psdInfo{}, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, psdInfo{}, err
	}
	info, err := readPSDInfo(f, st.Size())
	if err != nil {
		f.Close()
		return nil, psdInfo{}, err
	}
	return f, info, nil
}

// decodePSDForThumbnails decodes the image every thumbnail size of a PSD is made from.
func decodePSDForThumbnails(ctx context.Context, path string) (*image.NRGBA, error) {
	f, info, err := openPSD(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodePSD(ctx, f, info, psdBlock(info.width, info.height, psdThumbSide, true))
}

// psdFullBlock returns the block size for the "full" preview: 1 (every pixel) unless the image
// is over psdFullMaxPixels or JPEG's side limit.
func psdFullBlock(w, h int) int {
	k := 1
	for int64(w/k)*int64(h/k) > psdFullMaxPixels {
		k++
	}
	return max(k, psdBlock(w, h, psdFullMaxSide, false))
}

// flattenOnWhite composites img over white in place and returns it as an *image.RGBA (an
// opaque NRGBA has the same bytes), which image/jpeg encodes without per-pixel conversion.
// JPEG has no transparency, and the encoder would otherwise turn transparent areas black.
func flattenOnWhite(img *image.NRGBA) *image.RGBA {
	pix := img.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		a := uint32(pix[i+3])
		if a == 255 {
			continue
		}
		for c := 0; c < 3; c++ {
			pix[i+c] = uint8((uint32(pix[i+c])*a + 255*(255-a) + 127) / 255)
		}
		pix[i+3] = 255
	}
	return &image.RGBA{Pix: pix, Stride: img.Stride, Rect: img.Rect}
}

// psdPreview makes the preview of size previewSize of the PSD at path:
//   - small/large/xlarge: WebP, from the shared thumbnail image (encodeDecodedPreview, as for
//     every image), transparency kept;
//   - "original" (the viewer): JPEG, fitted in psdOriginalSide;
//   - "full" (the viewer's full-size button): JPEG, every pixel, within psdFullMaxPixels.
//
// The viewer sizes are flattened onto white (see psdViewJPEGQuality for why JPEG).
func psdPreview(ctx context.Context, path, previewSize string) ([]byte, error) {
	switch previewSize {
	case "original", "full":
		f, info, err := openPSD(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		side, k := psdOriginalSide, psdBlock(info.width, info.height, psdOriginalSide, false)
		if previewSize == "full" {
			side, k = psdFullMaxSide, psdFullBlock(info.width, info.height)
		}
		img, err := decodePSD(ctx, f, info, k)
		if err != nil {
			return nil, err
		}
		if b := img.Bounds(); b.Dx() > side || b.Dy() > side {
			fitted := imaging.Fit(img, side, side, QualityHigh.resampleFilter())
			if n, ok := fitted.(*image.NRGBA); ok {
				img = n
			} else {
				img = imaging.Clone(fitted)
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var out bytes.Buffer
		if err := jpeg.Encode(&out, flattenOnWhite(img), &jpeg.Options{Quality: psdViewJPEGQuality}); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	default:
		opts, err := getPreviewOptions(previewSize)
		if err != nil {
			return nil, err
		}
		opts.Format = FormatWebp
		opts = resizeDefaults(opts)
		img, err := decodePSDForThumbnails(ctx, path)
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		if err := encodeDecodedPreview(img, &out, opts); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
}
