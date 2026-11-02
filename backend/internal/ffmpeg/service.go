package ffmpeg

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	goffmpeg "github.com/gtsteffaniak/go-ffmpeg"
	"github.com/gtsteffaniak/go-ffmpeg/capabilities"
	"github.com/gtsteffaniak/go-ffmpeg/encode"
	ffexec "github.com/gtsteffaniak/go-ffmpeg/exec"
	"github.com/gtsteffaniak/go-ffmpeg/ops"
	"github.com/gtsteffaniak/go-logger/logger"
)

// Service wraps go-ffmpeg for filebrowser media operations.
type Service struct {
	inner    *goffmpeg.Service
	cacheDir string
}

// FFmpegService is kept for existing callers.
type FFmpegService = Service

var global *Service

// InitOptions configures startup initialization.
type InitOptions struct {
	FFmpegPath           string
	MaxConcurrent        int // deprecated: use Concurrency
	Concurrency          goffmpeg.Concurrency
	CacheDir             string
	SkipHWTests          bool
	HardwareAcceleration bool
}

// Initialize creates the global ffmpeg service and runs capability detection.
func Initialize(ctx context.Context, opts InitOptions) error {
	if opts.CacheDir == "" {
		opts.CacheDir = os.TempDir()
	}

	if opts.HardwareAcceleration {
		logger.Info("Detecting ffmpeg hardware codec support...")
	}

	cfg := goffmpeg.Config{
		FFmpegPath:    opts.FFmpegPath,
		Concurrency:   opts.Concurrency,
		Logger:        goffmpeg.NopLogger(),
		SkipHWTests:   opts.SkipHWTests,
	}
	if opts.Concurrency.MaxProbe == 0 && opts.Concurrency.MaxDecode == 0 && opts.Concurrency.MaxEncode == 0 {
		if opts.MaxConcurrent < 1 {
			opts.MaxConcurrent = 4
		}
		cfg.MaxConcurrent = opts.MaxConcurrent
	}

	svc, err := goffmpeg.New(ctx, cfg)
	if err != nil {
		global = nil
		return err
	}

	global = &Service{
		inner:    svc,
		cacheDir: opts.CacheDir,
	}

	logCapabilities(svc, opts.HardwareAcceleration)
	return nil
}

// Get returns the initialized service, or nil when ffmpeg is unavailable.
func Get() *Service {
	return global
}

// Enabled reports whether ffmpeg initialized successfully.
func Enabled() bool {
	return global != nil && global.inner != nil
}

// FFmpegPath returns the resolved ffmpeg binary path.
func (s *Service) FFmpegPath() string {
	if s == nil || s.inner == nil {
		return ""
	}
	return s.inner.FFmpegPath()
}

// FFprobePath returns the resolved ffprobe binary path.
func (s *Service) FFprobePath() string {
	if s == nil || s.inner == nil {
		return ""
	}
	return s.inner.FFprobePath()
}

func (s *Service) Acquire(ctx context.Context) error {
	if s == nil || s.inner == nil {
		return fmt.Errorf("ffmpeg service not available")
	}
	return s.inner.Acquire(ctx)
}

func (s *Service) Release() {
	if s == nil || s.inner == nil {
		return
	}
	s.inner.Release()
}

// VideoPreview extracts a JPEG preview frame to w.
// go-ffmpeg Service methods acquire concurrency slots automatically; do not Acquire before calling.
func (s *Service) VideoPreview(ctx context.Context, w io.Writer, opts ops.PreviewOptions) error {
	if s == nil || s.inner == nil {
		return fmt.Errorf("ffmpeg service not available")
	}
	return s.inner.VideoPreview(ctx, w, opts)
}

// VideoPreviewKeyframe extracts a JPEG preview frame to w like VideoPreview, but
// decodes keyframes only (-skip_frame nokey) and uses the keyframe at or before the
// seek point (-noaccurate_seek) instead of decoding up to the exact frame. This is much
// faster for high-resolution video with long or open GOPs (common with HEVC), where
// exact seeking can exceed the preview timeout. Otherwise it builds the same command as
// go-ffmpeg's ops.VideoPreviewFrame (scale filter, quality, protocol whitelist) and
// holds one decode slot while ffmpeg runs.
//
// It can succeed with empty output, e.g. when the seek point lies after the last
// keyframe; callers should then fall back to VideoPreview.
func (s *Service) VideoPreviewKeyframe(ctx context.Context, w io.Writer, opts ops.PreviewOptions) error {
	if s == nil || s.inner == nil {
		return fmt.Errorf("ffmpeg service not available")
	}

	dur := opts.DurationSec
	if dur <= 0 {
		var err error
		dur, err = s.GetMediaDuration(ctx, opts.Input)
		if err != nil {
			return err
		}
	}
	seekSec := ops.ResolvePreviewSeekSec(dur, opts)

	args := []string{"-hide_banner", "-nostats", "-nostdin",
		"-skip_frame", "nokey", "-noaccurate_seek",
		"-ss", fmt.Sprintf("%.3f", seekSec),
		"-protocol_whitelist", ffexec.DefaultProtocolWhitelist,
		"-i", opts.Input,
	}
	if opts.Width > 0 && opts.Height > 0 {
		args = append(args, "-vf", ops.PreviewScaleFilter(opts.Width, opts.Height, opts.ScaleMode))
	}
	q := opts.Quality
	if q == 0 {
		q = 10
	}
	args = append(args,
		"-frames:v", "1",
		"-q:v", encode.QualityToQScale(q),
		"-vcodec", "mjpeg",
		"-f", "image2",
		"-",
	)

	if err := s.inner.Acquire(ctx); err != nil {
		return err
	}
	defer s.inner.Release()

	cmd := exec.CommandContext(ctx, s.inner.FFmpegPath(), args...)
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("keyframe preview failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ExtractSubtitle returns embedded subtitle content as WebVTT.
func (s *Service) ExtractSubtitle(ctx context.Context, videoPath string, streamIndex int) (string, error) {
	if s == nil || s.inner == nil {
		return "", fmt.Errorf("ffmpeg service not available")
	}

	fileInfo, err := os.Stat(videoPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	cacheKey := fmt.Sprintf("subtitle_content:%s:%d:%d", videoPath, streamIndex, fileInfo.ModTime().Unix())
	if content, ok := SubtitleContentCache.Get(cacheKey); ok {
		return content, nil
	}

	content, err := s.inner.ExtractSubtitle(ctx, videoPath, streamIndex)
	if err != nil {
		return "", err
	}

	SubtitleContentCache.Set(cacheKey, content)
	return content, nil
}

func logCapabilities(svc *goffmpeg.Service, detectHardware bool) {
	caps := svc.Capabilities()
	if caps == nil {
		return
	}

	logger.Debugf("Media Enabled: version %s @ %s", caps.FFmpegVersion, caps.FFmpegPath)

	if !detectHardware {
		return
	}

	hw := hardwareCodecSummary(svc)
	if hw == "" {
		logger.Warning("no ffmpeg hardware codec support found")
		return
	}
	logger.Debugf("supported ffmpeg hardware codecs: %s", hw)
}

func hardwareCodecSummary(svc *goffmpeg.Service) string {
	seen := make(map[string]struct{})
	var parts []string

	add := func(entry string) {
		if entry == "" {
			return
		}
		if _, ok := seen[entry]; ok {
			return
		}
		seen[entry] = struct{}{}
		parts = append(parts, entry)
	}

	for _, opt := range svc.AvailableEncodeOptions() {
		if opt.Accel == capabilities.AccelNone {
			continue
		}
		add(fmt.Sprintf("%s encode via %s (%s)", opt.Codec, opt.Encoder, capabilities.AccelLabel(opt.Accel)))
	}
	for _, opt := range svc.AvailableDecodeOptions() {
		if opt.Accel == capabilities.AccelNone {
			continue
		}
		add(fmt.Sprintf("%s decode via %s (%s)", opt.Codec, opt.Decoder, capabilities.AccelLabel(opt.Accel)))
	}

	return strings.Join(parts, ", ")
}
