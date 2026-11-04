package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	goffmpeg "github.com/gtsteffaniak/go-ffmpeg"
	ffexec "github.com/gtsteffaniak/go-ffmpeg/exec"
)

var blockedMediaPathPrefixes = []string{
	"pipe:",
	"concat:",
	"subfile:",
	"crypto:",
	"data:",
}

// resolveLocalMediaPath restricts ffprobe to absolute local files (no remote protocols).
func resolveLocalMediaPath(mediaPath string) (string, os.FileInfo, error) {
	trimmed := strings.TrimSpace(mediaPath)
	if trimmed == "" {
		return "", nil, fmt.Errorf("media path is empty")
	}
	if strings.Contains(trimmed, "://") {
		return "", nil, fmt.Errorf("remote media URLs are not allowed")
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range blockedMediaPathPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return "", nil, fmt.Errorf("remote media protocols are not allowed")
		}
	}

	clean := filepath.Clean(trimmed)
	if !filepath.IsAbs(clean) {
		return "", nil, fmt.Errorf("media path must be absolute")
	}

	info, err := os.Stat(clean)
	if err != nil {
		return "", nil, fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("media path is a directory")
	}
	return clean, info, nil
}

// FileProbeInfo holds ffprobe results for a local media file.
type FileProbeInfo struct {
	Duration   float64
	VideoCodec string
	AudioCodec string
	FormatName string
}

// ProbeFile extracts duration and codec info from a media file.
//
// fufu: results are cached per file (path, modification time and size), and the file is
// first probed from its container header only (probeFileHeader). A full probe, which
// decodes the start of every stream and costs far more CPU, only runs when the header
// gives no duration (some formats, such as MPEG-TS, store none).
func (s *Service) ProbeFile(ctx context.Context, mediaPath string) (*FileProbeInfo, error) {
	if s == nil || s.inner == nil {
		return nil, fmt.Errorf("ffmpeg service not available")
	}

	localPath, fileInfo, err := resolveLocalMediaPath(mediaPath)
	if err != nil {
		return nil, err
	}
	cacheKey := fmt.Sprintf("%s:%d:%d", localPath, fileInfo.ModTime().UnixNano(), fileInfo.Size())
	if cached, ok := ProbeCache.Get(cacheKey); ok {
		info := cached
		return &info, nil
	}

	info, err := s.probeFileHeader(ctx, localPath)
	if err != nil || info.Duration <= 0 {
		info, err = s.probeFileFull(ctx, localPath)
		if err != nil {
			return nil, err
		}
	}
	ProbeCache.Set(cacheKey, *info)
	return info, nil
}

// probeFileFull runs go-ffmpeg's full probe (ffprobe -show_streams -show_format).
func (s *Service) probeFileFull(ctx context.Context, localPath string) (*FileProbeInfo, error) {
	info, err := s.inner.ProbeStream(ctx, goffmpeg.ProbeStreamOptions{
		URL:        localPath,
		StreamType: goffmpeg.StreamFile,
		Timeout:    30 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	return &FileProbeInfo{
		Duration:   info.Duration,
		VideoCodec: info.VideoCodec,
		AudioCodec: info.AudioCodec,
		FormatName: info.FormatName,
	}, nil
}

// headerProbeTimeout bounds probeFileHeader; the full probe that follows a failure has its
// own 30 s timeout.
const headerProbeTimeout = 10 * time.Second

// probeFileHeader reads duration and codecs from the container header only
// (-nofind_stream_info: no decoding). It holds one probe slot while ffprobe runs.
func (s *Service) probeFileHeader(ctx context.Context, localPath string) (*FileProbeInfo, error) {
	args := []string{
		"-v", "error",
		"-nofind_stream_info",
		"-protocol_whitelist", ffexec.DefaultProtocolWhitelist,
		"-print_format", "json",
		"-show_entries", "format=duration,format_name:stream=codec_type,codec_name,duration",
		localPath,
	}

	if err := s.inner.Acquire(ctx, goffmpeg.SlotProbe); err != nil {
		return nil, err
	}
	defer s.inner.Release(goffmpeg.SlotProbe)

	pctx, cancel := context.WithTimeout(ctx, headerProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(pctx, s.inner.FFprobePath(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if pctx.Err() != nil {
			return nil, fmt.Errorf("header probe: %w", pctx.Err())
		}
		return nil, fmt.Errorf("header probe failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseHeaderProbe(stdout.Bytes())
}

// parseHeaderProbe turns probeFileHeader's ffprobe JSON into a FileProbeInfo. The duration
// is the container's when it has one, else the longest stream's (MP4 stores durations per
// track; the overall one is only computed by a full probe). Codecs follow go-ffmpeg's full
// probe: the first video stream and the first audio stream.
func parseHeaderProbe(out []byte) (*FileProbeInfo, error) {
	var result struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Duration  string `json:"duration"`
		} `json:"streams"`
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("header probe output: %w", err)
	}
	if len(result.Streams) == 0 {
		return nil, fmt.Errorf("header probe found no streams")
	}

	info := &FileProbeInfo{FormatName: result.Format.FormatName}
	longest := 0.0
	for _, st := range result.Streams {
		if d := parseProbeSeconds(st.Duration); d > longest {
			longest = d
		}
		switch st.CodecType {
		case "video":
			if info.VideoCodec == "" {
				info.VideoCodec = st.CodecName
			}
		case "audio":
			if info.AudioCodec == "" {
				info.AudioCodec = st.CodecName
			}
		}
	}
	info.Duration = parseProbeSeconds(result.Format.Duration)
	if info.Duration <= 0 {
		info.Duration = longest
	}
	return info, nil
}

// parseProbeSeconds parses an ffprobe duration ("12.345000"); 0 for "", "N/A" or junk.
func parseProbeSeconds(v string) float64 {
	d, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || d <= 0 || math.IsInf(d, 0) || math.IsNaN(d) {
		return 0
	}
	return d
}

// GetMediaDuration extracts the duration of a media file in seconds.
func (s *Service) GetMediaDuration(ctx context.Context, mediaPath string) (float64, error) {
	if s == nil || s.inner == nil {
		return 0, fmt.Errorf("ffmpeg service not available")
	}

	localPath, fileInfo, err := resolveLocalMediaPath(mediaPath)
	if err != nil {
		return 0, err
	}
	cacheKey := fmt.Sprintf("%s:%d", localPath, fileInfo.ModTime().Unix())
	if duration, found := MetadataCache.Get(cacheKey); found {
		return duration, nil
	}

	duration, err := s.inner.GetMediaDuration(ctx, localPath)
	if err != nil {
		return 0, err
	}

	MetadataCache.Set(cacheKey, duration)
	return duration, nil
}
