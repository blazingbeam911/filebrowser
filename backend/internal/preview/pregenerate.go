package preview

// fufu: filling the thumbnail cache ahead of browsing, for the thumbnail job
// (internal/thumbjob). Thumbnails are stored under the same keys GetPreviewForFile reads, so a
// later browse request is a cache hit. Plain images that need resizing to every missing size
// are decoded once for all of them; everything else goes through upstream's own per-size path.

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
	"github.com/kovidgoyal/imaging"
)

// PregenResult counts what Pregenerate did with one file's sizes.
type PregenResult struct {
	Generated int // sizes made and stored
	Cached    int // sizes that were already in the cache
}

// errNoService is returned when the preview service hasn't been started.
var errNoService = errors.New("preview service is not running")

// cacheHashFor is the cache hash GetPreviewForFile computes for a file: the hash of the album
// art for audio that has it, otherwise FileKey of the file on disk.
func cacheHashFor(file iteminfo.ExtendedFileInfo) (string, error) {
	if file.Metadata != nil && len(file.Metadata.AlbumArt) > 0 {
		sum := md5.Sum(file.Metadata.AlbumArt)
		return hex.EncodeToString(sum[:]), nil
	}
	hash, _, err := FileKey(file.RealPath)
	return hash, err
}

// statSizer is a cache that can tell an entry's size without reading it (diskcache.FileCache).
type statSizer interface {
	StatSize(key string) (int64, bool, error)
}

// CachedFromStat reports whether every given size (at seek 0) is already in the cache for the
// file at realPath, which the caller has stat'ed as info: the key GetPreviewForFile computes
// for a file without album art. It only stats the cache entries; an entry under
// minPreviewSize (the empty marker of a failed attempt) counts as not cached, so the file
// goes the full way.
func CachedFromStat(realPath string, info os.FileInfo, sizes []string) bool {
	if service == nil {
		return false
	}
	sc, ok := service.fileCache.(statSizer)
	if !ok {
		return false
	}
	hash, _, err := FileKeyFromInfo(realPath, info)
	if err != nil {
		return false
	}
	for _, s := range sizes {
		n, found, err := sc.StatSize(CacheKey(hash, s, 0))
		if err != nil || !found || n < minPreviewSize {
			return false
		}
	}
	return true
}

// Pregenerate makes sure the thumbnails of file in the given sizes (at seek 0) are in the
// cache. An earlier failed attempt for this version of the file (the empty marker the preview
// path stores) is reported as ErrPreviewTooSmall without trying again, as browsing would.
func Pregenerate(ctx context.Context, file iteminfo.ExtendedFileInfo, sizes []string) (PregenResult, error) {
	var res PregenResult
	if service == nil {
		return res, errNoService
	}
	if !file.HasPreview {
		return res, ErrUnsupportedMedia
	}
	hash, err := cacheHashFor(file)
	if err != nil {
		return res, err
	}
	var missing []string
	for _, size := range sizes {
		data, found, err := service.fileCache.Load(ctx, CacheKey(hash, size, 0))
		if err != nil {
			return res, fmt.Errorf("failed to load from cache: %w", err)
		}
		switch {
		case !found:
			missing = append(missing, size)
		case len(data) < minPreviewSize:
			return res, ErrPreviewTooSmall
		default:
			res.Cached++
		}
	}
	if len(missing) == 0 {
		return res, nil
	}
	if len(missing) > 1 && decodeOnceEligible(file, missing) {
		made, rest := service.pregenerateFromOneDecode(ctx, file, hash, missing)
		res.Generated += made
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		missing = rest
	}
	// The rest (and anything the single decode couldn't do, e.g. a JPEG that needs the FFmpeg
	// fallback) takes upstream's path, one size at a time.
	for _, size := range missing {
		if _, err := GeneratePreviewWithMD5(ctx, file, size, "", 0, hash); err != nil {
			return res, err
		}
		res.Generated++
	}
	return res, nil
}

// decodeOnceEligible reports whether every size of file would be made by resizing the decoded
// image (generateImagePreview's path), so one decode can serve them all.
func decodeOnceEligible(file iteminfo.ExtendedFileInfo, sizes []string) bool {
	const maxSizeForOriginal = 256 * 1024 // generateImagePreview's limit
	if determinePreviewType(file) != previewTypeImage || hasEmbeddedPreview(file.Type, file.Name) {
		return false
	}
	if file.Size < maxSizeForOriginal || file.Size > iteminfo.LargeFileSizeThreshold {
		return false
	}
	if _, err := service.FormatFromExtension(strings.ToLower(filepath.Ext(file.Name))); err != nil {
		return false
	}
	w, h, err := ReadImageFileDimensions(file.RealPath)
	if err != nil {
		return false
	}
	for _, size := range sizes {
		if size == "original" || ImageFitsPreviewSize(w, h, size) {
			return false
		}
	}
	return true
}

// pregenerateFromOneDecode stores each size of an image from a single decode, under the image
// semaphore GeneratePreviewWithMD5 would take. It returns how many sizes it stored and the
// sizes it couldn't make (to be tried through upstream's path).
func (s *Service) pregenerateFromOneDecode(ctx context.Context, file iteminfo.ExtendedFileInfo, hash string, sizes []string) (int, []string) {
	const largeFileSizeThreshold = 8 * 1024 * 1024 // GeneratePreviewWithMD5's split
	if file.Size >= largeFileSizeThreshold && s.imageLargeSem != nil {
		if err := s.acquireImageLargeSem(ctx); err != nil {
			return 0, sizes
		}
		defer s.releaseImageLargeSem()
	} else {
		if err := s.acquireImageSem(ctx); err != nil {
			return 0, sizes
		}
		defer s.releaseImageSem()
	}

	f, err := os.Open(file.RealPath)
	if err != nil {
		return 0, sizes
	}
	defer f.Close()
	format, err := s.FormatFromExtension(strings.ToLower(filepath.Ext(file.Name)))
	if err != nil {
		return 0, sizes
	}

	made := 0
	var rest []string
	var img image.Image
	for _, size := range sizes {
		if ctx.Err() != nil {
			rest = append(rest, size)
			continue
		}
		opts, err := getPreviewOptions(size)
		if err != nil {
			rest = append(rest, size)
			continue
		}
		opts.Format = format
		opts = resizeDefaults(opts)

		var out []byte
		// Low-quality JPEG thumbnails use the embedded EXIF thumbnail when there is one,
		// as resizeWithSize does.
		if opts.Format == FormatJpeg && opts.Quality == QualityLow {
			if _, err := f.Seek(0, io.SeekStart); err == nil {
				if thm, _, errThm := getEmbeddedThumbnail(f); errThm == nil && len(thm) > 0 {
					out = thm
				}
			}
		}
		if out == nil {
			if img == nil {
				if _, err := f.Seek(0, io.SeekStart); err != nil {
					return made, append(rest, remainingFrom(sizes, size)...)
				}
				if img, err = decodeForPreview(f, opts.Format); err != nil {
					return made, append(rest, remainingFrom(sizes, size)...)
				}
			}
			var buf bytes.Buffer
			if err := encodeDecodedPreview(img, &buf, opts); err != nil {
				rest = append(rest, size)
				continue
			}
			out = buf.Bytes()
		}
		if len(out) < minPreviewSize {
			rest = append(rest, size) // upstream's path records the failure the same way browsing would
			continue
		}
		if err := s.fileCache.Store(ctx, CacheKey(hash, size, 0), out); err != nil {
			rest = append(rest, size)
			continue
		}
		made++
	}
	return made, rest
}

// remainingFrom returns size and the sizes after it.
func remainingFrom(sizes []string, size string) []string {
	for i, s := range sizes {
		if s == size {
			return sizes[i:]
		}
	}
	return nil
}

// The three functions below repeat resizeWithSize (image.go) split into its decode and its
// resize-and-encode halves. TestPregenerateMatchesBrowsing compares their output byte for
// byte with the browse path's, so a change upstream makes to resizeWithSize shows up there.

// resizeDefaults applies resizeWithSize's defaults and format choice.
func resizeDefaults(opts ResizeOptions) ResizeOptions {
	if opts.ResizeMode == 0 {
		opts.ResizeMode = ResizeModeFit
	}
	if opts.Quality == 0 {
		opts.Quality = QualityMedium
	}
	opts.Format = thumbnailFormat(opts.Format)
	if !opts.Format.encodable() {
		opts.Format = FormatJpeg
	}
	if opts.JpegQuality == 0 {
		opts.JpegQuality = opts.Quality.jpegQuality()
	}
	return opts
}

// decodeForPreview decodes as resizeWithSize does (decodePreviewImage, image_decode.go).
func decodeForPreview(r io.ReadSeeker, format Format) (image.Image, error) {
	return decodePreviewImage(r, format)
}

// encodeDecodedPreview resizes and encodes a decoded image as resizeWithSize does.
func encodeDecodedPreview(img image.Image, out io.Writer, opts ResizeOptions) error {
	bounds := img.Bounds()
	if !(opts.ResizeMode != ResizeModeFill && bounds.Dx() <= opts.Width && bounds.Dy() <= opts.Height) {
		resampleFilter := opts.Quality.resampleFilter()
		switch opts.ResizeMode {
		case ResizeModeFill:
			img = imaging.Fill(img, opts.Width, opts.Height, imaging.Center, resampleFilter)
		default:
			img = imaging.Fit(img, opts.Width, opts.Height, resampleFilter)
		}
	}
	switch opts.Format {
	case FormatWebp:
		return encodeWebP(out, img, opts.Quality)
	case FormatJpeg:
		return jpeg.Encode(out, img, &jpeg.Options{Quality: opts.JpegQuality})
	default:
		return imaging.Encode(out, img, opts.Format.toImaging())
	}
}
