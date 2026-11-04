package ffmpeg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	goffmpeg "github.com/gtsteffaniak/go-ffmpeg"
)

func TestParseHeaderProbe(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    FileProbeInfo
		wantErr bool
	}{
		{
			name: "mp4: no container duration, longest track wins",
			json: `{"streams":[{"codec_type":"video","codec_name":"h264","duration":"130.583333"},
				{"codec_type":"audio","codec_name":"aac","duration":"130.600000"}],
				"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2"}}`,
			want: FileProbeInfo{Duration: 130.6, VideoCodec: "h264", AudioCodec: "aac", FormatName: "mov,mp4,m4a,3gp,3g2,mj2"},
		},
		{
			name: "mkv: container duration, none per track",
			json: `{"streams":[{"codec_type":"video","codec_name":"hevc"},{"codec_type":"audio","codec_name":"opus"},
				{"codec_type":"subtitle","codec_name":"ass"}],
				"format":{"format_name":"matroska,webm","duration":"1420.032000"}}`,
			want: FileProbeInfo{Duration: 1420.032, VideoCodec: "hevc", AudioCodec: "opus", FormatName: "matroska,webm"},
		},
		{
			name: "container duration is preferred over track durations",
			json: `{"streams":[{"codec_type":"video","codec_name":"h264","duration":"99.0"}],
				"format":{"format_name":"mov","duration":"100.5"}}`,
			want: FileProbeInfo{Duration: 100.5, VideoCodec: "h264", FormatName: "mov"},
		},
		{
			name: "first video and first audio stream give the codecs",
			json: `{"streams":[{"codec_type":"video","codec_name":"hevc","duration":"10"},
				{"codec_type":"video","codec_name":"mjpeg","duration":"10"},
				{"codec_type":"audio","codec_name":"aac","duration":"10"},
				{"codec_type":"audio","codec_name":"ac3","duration":"10"},
				{"codec_type":"data","codec_name":"unknown"}],"format":{"format_name":"mov"}}`,
			want: FileProbeInfo{Duration: 10, VideoCodec: "hevc", AudioCodec: "aac", FormatName: "mov"},
		},
		{
			name: "no duration anywhere gives 0 (caller falls back to a full probe)",
			json: `{"streams":[{"codec_type":"video","codec_name":"mpeg2video","duration":"N/A"}],
				"format":{"format_name":"mpegts","duration":"N/A"}}`,
			want: FileProbeInfo{VideoCodec: "mpeg2video", FormatName: "mpegts"},
		},
		{name: "no streams", json: `{"streams":[],"format":{"format_name":"mov"}}`, wantErr: true},
		{name: "not JSON", json: `Invalid data found when processing input`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHeaderProbe([]byte(tt.json))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseHeaderProbe() = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseHeaderProbe() error = %v", err)
			}
			if *got != tt.want {
				t.Fatalf("parseHeaderProbe() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestParseProbeSeconds(t *testing.T) {
	for in, want := range map[string]float64{
		"12.5": 12.5, " 3 ": 3, "": 0, "N/A": 0, "-1": 0, "0": 0, "inf": 0, "nan": 0, "x": 0,
	} {
		if got := parseProbeSeconds(in); got != want {
			t.Errorf("parseProbeSeconds(%q) = %v, want %v", in, got, want)
		}
	}
}

func initProbeTestService(t *testing.T) *Service {
	t.Helper()
	requireFFmpegBinaries(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := Initialize(ctx, InitOptions{
		Concurrency: goffmpeg.Concurrency{MaxProbe: 2, MaxDecode: 2, MaxEncode: 1},
		CacheDir:    t.TempDir(),
		SkipHWTests: true,
	})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	t.Cleanup(func() { global = nil })
	return Get()
}

func makeClip(t *testing.T, name string, extra ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	args := append([]string{"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:d=3",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-shortest"}, extra...)
	args = append(args, "-y", out)
	if err := exec.Command("ffmpeg", args...).Run(); err != nil {
		t.Skipf("could not generate %s: %v", name, err)
	}
	return out
}

func TestProbeFileHeaderOnly(t *testing.T) {
	svc := initProbeTestService(t)
	clip := makeClip(t, "clip.mp4", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac")

	info, err := svc.probeFileHeader(context.Background(), clip)
	if err != nil {
		t.Fatalf("probeFileHeader() error = %v", err)
	}
	if info.Duration < 2.9 || info.Duration > 3.2 {
		t.Errorf("header duration = %v, want about 3", info.Duration)
	}
	if info.VideoCodec != "h264" || info.AudioCodec != "aac" {
		t.Errorf("header codecs = %q/%q, want h264/aac", info.VideoCodec, info.AudioCodec)
	}

	full, err := svc.probeFileFull(context.Background(), clip)
	if err != nil {
		t.Fatalf("probeFileFull() error = %v", err)
	}
	if d := full.Duration - info.Duration; d > 0.5 || d < -0.5 {
		t.Errorf("header duration %v differs from full probe %v", info.Duration, full.Duration)
	}
	if full.VideoCodec != info.VideoCodec || full.AudioCodec != info.AudioCodec || full.FormatName != info.FormatName {
		t.Errorf("header probe %+v differs from full probe %+v", *info, *full)
	}
}

func TestProbeFileFallsBackWithoutHeaderDuration(t *testing.T) {
	svc := initProbeTestService(t)
	clip := makeClip(t, "clip.ts", "-c:v", "mpeg2video", "-c:a", "mp2", "-f", "mpegts")

	header, err := svc.probeFileHeader(context.Background(), clip)
	if err == nil && header.Duration > 0 {
		t.Skipf("this ffprobe reads a duration from the MPEG-TS header (%v); fallback not exercised", header.Duration)
	}

	info, err := svc.ProbeFile(context.Background(), clip)
	if err != nil {
		t.Fatalf("ProbeFile() error = %v", err)
	}
	if info.Duration < 2.5 || info.Duration > 3.5 {
		t.Errorf("ProbeFile() duration = %v, want about 3 from the full probe", info.Duration)
	}
}

func TestProbeFileCache(t *testing.T) {
	svc := initProbeTestService(t)
	clip := makeClip(t, "clip.mp4", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac")

	first, err := svc.ProbeFile(context.Background(), clip)
	if err != nil {
		t.Fatalf("ProbeFile() error = %v", err)
	}

	// Make the file unprobeable while keeping its size and modification time: a cache hit
	// still returns the first result, a real probe would fail.
	st, err := os.Stat(clip)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clip, make([]byte, st.Size()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(clip, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.probeFileFull(context.Background(), clip); err == nil {
		t.Fatal("the zeroed file still probes; the test can't tell a cache hit apart")
	}

	second, err := svc.ProbeFile(context.Background(), clip)
	if err != nil {
		t.Fatalf("cached ProbeFile() error = %v", err)
	}
	if *second != *first {
		t.Errorf("cached ProbeFile() = %+v, want %+v", *second, *first)
	}
	second.Duration = -1 // the caller's copy must not alter the cache
	if third, err := svc.ProbeFile(context.Background(), clip); err != nil || third.Duration != first.Duration {
		t.Errorf("cache entry changed through a returned pointer: %+v, %v", third, err)
	}

	// A new modification time means a changed file: probed again (and now failing).
	later := st.ModTime().Add(time.Hour)
	if err := os.Chtimes(clip, later, later); err != nil {
		t.Fatal(err)
	}
	if info, err := svc.ProbeFile(context.Background(), clip); err == nil {
		t.Errorf("ProbeFile() after the file changed = %+v from the cache, want a fresh (failing) probe", info)
	}
}
