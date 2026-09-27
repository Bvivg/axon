package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	MaxVideoSide = 1920
	PosterSide   = 480

	MaxVoiceDuration = 15 * time.Minute

	VideoDemuxers = "mov,mp4,m4a,3gp,3g2,mj2,matroska,webm,avi,ogg,mpegts"
	VoiceDemuxers = "mov,mp4,m4a,3gp,matroska,webm,ogg,wav,mp3,aac,flac"
	MP4Demuxer    = "mov"
)

var ErrToolsMissing = errors.New("media: ffmpeg is not available")

type ToolsConfig struct {
	FFmpeg  string
	FFprobe string

	Workers int
	Timeout time.Duration
}

type Tools struct {
	ffmpeg  string
	ffprobe string
	timeout time.Duration
	slots   chan struct{}
}

func NewTools(cfg ToolsConfig) *Tools {
	workers := cfg.Workers
	if workers <= 0 {
		workers = 1
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	return &Tools{
		ffmpeg:  cfg.FFmpeg,
		ffprobe: cfg.FFprobe,
		timeout: timeout,
		slots:   make(chan struct{}, workers),
	}
}

func (t *Tools) Check() error {
	for _, bin := range []string{t.ffmpeg, t.ffprobe} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%w: %w", ErrToolsMissing, err)
		}
	}
	return nil
}

type Probe struct {
	Format     string
	Width      int
	Height     int
	Duration   time.Duration
	HasVideo   bool
	HasAudio   bool
	StillImage bool
}

type probeOutput struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

func (t *Tools) Probe(ctx context.Context, path, demuxers string) (Probe, error) {
	out, err := t.run(ctx, t.ffprobe,
		"-v", "error",
		"-protocol_whitelist", "file",
		"-format_whitelist", demuxers,
		"-print_format", "json",
		"-show_streams", "-show_format",
		"file:"+path,
	)
	if err != nil {
		return Probe{}, fmt.Errorf("%w: %w", ErrUnsupported, err)
	}

	var parsed probeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return Probe{}, fmt.Errorf("%w: probe output: %w", ErrUnsupported, err)
	}

	probe := Probe{Format: parsed.Format.FormatName}
	if seconds, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil && !math.IsNaN(seconds) && seconds > 0 {
		probe.Duration = time.Duration(seconds * float64(time.Second))
	}
	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			if s.Disposition.AttachedPic == 1 || probe.HasVideo {
				continue
			}
			probe.HasVideo = true
			probe.Width, probe.Height = s.Width, s.Height
			probe.StillImage = isStillCodec(s.CodecName)
		case "audio":
			probe.HasAudio = true
		}
	}
	return probe, nil
}

func (t *Tools) TranscodeVideo(ctx context.Context, in, out string) error {
	scale := fmt.Sprintf(
		"scale=w='if(gte(iw,ih),trunc(min(%[1]d,iw)/2)*2,-2)':h='if(gte(iw,ih),-2,trunc(min(%[1]d,ih)/2)*2)':flags=lanczos,format=yuv420p",
		MaxVideoSide,
	)
	_, err := t.run(ctx, t.ffmpeg,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-protocol_whitelist", "file",
		"-format_whitelist", VideoDemuxers,
		"-i", "file:"+in,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-sn", "-dn",
		"-vf", scale,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-c:a", "aac", "-b:a", "128k", "-ac", "2",
		"-threads", "2",
		"-map_metadata", "-1", "-map_chapters", "-1",
		"-fflags", "+bitexact", "-flags:v", "+bitexact", "-flags:a", "+bitexact",
		"-movflags", "+faststart",
		"-f", "mp4", "file:"+out,
	)
	if err != nil {
		return fmt.Errorf("%w: transcode video: %w", ErrUnsupported, err)
	}
	return nil
}

func (t *Tools) Poster(ctx context.Context, in, out string, at time.Duration) error {
	_, err := t.run(ctx, t.ffmpeg,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-protocol_whitelist", "file",
		"-format_whitelist", MP4Demuxer,
		"-ss", strconv.FormatFloat(at.Seconds(), 'f', 3, 64),
		"-i", "file:"+in,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=w='min(%[1]d,iw)':h='min(%[1]d,ih)':force_original_aspect_ratio=decrease", PosterSide),
		"-q:v", "4",
		"-map_metadata", "-1",
		"-update", "1",
		"-f", "image2", "-c:v", "mjpeg", "file:"+out,
	)
	if err != nil {
		return fmt.Errorf("media: poster: %w", err)
	}
	return nil
}

func (t *Tools) TranscodeVoice(ctx context.Context, in, out string) error {
	_, err := t.run(ctx, t.ffmpeg,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-protocol_whitelist", "file",
		"-format_whitelist", VoiceDemuxers,
		"-i", "file:"+in,
		"-map", "0:a:0", "-vn", "-sn", "-dn",
		"-ac", "1", "-ar", "48000",
		"-c:a", "aac", "-b:a", "48k",
		"-map_metadata", "-1", "-map_chapters", "-1",
		"-fflags", "+bitexact", "-flags:a", "+bitexact",
		"-movflags", "+faststart",
		"-f", "mp4", "file:"+out,
	)
	if err != nil {
		return fmt.Errorf("%w: transcode voice: %w", ErrUnsupported, err)
	}
	return nil
}

func (t *Tools) run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	select {
	case t.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-t.slots }()

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%s: %w: %s", bin, err, lastLine(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func isStillCodec(codec string) bool {
	switch codec {
	case "mjpeg", "png", "bmp", "gif", "webp", "tiff", "hevc_image":
		return true
	default:
		return false
	}
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	const limit = 200
	if len(s) > limit {
		s = s[:limit]
	}
	return s
}
