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

	DefaultMaxVideoPixels = 36_000_000

	VideoDemuxers = "mov,mp4,m4a,3gp,3g2,mj2,matroska,webm,avi,ogg,mpegts"
	VoiceDemuxers = "mov,mp4,m4a,3gp,matroska,webm,ogg,wav,mp3,aac,flac"
	MP4Demuxer    = "mov"
)

var ErrToolsMissing = errors.New("media: ffmpeg is not available")

type ToolsConfig struct {
	FFmpeg  string
	FFprobe string

	Workers        int
	PerUser        int
	Timeout        time.Duration
	MaxVideoPixels int64
}

type Tools struct {
	ffmpeg         string
	ffprobe        string
	timeout        time.Duration
	maxVideoPixels int64
	slots          *Slots
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
	maxVideoPixels := cfg.MaxVideoPixels
	if maxVideoPixels <= 0 {
		maxVideoPixels = DefaultMaxVideoPixels
	}
	return &Tools{
		ffmpeg:         cfg.FFmpeg,
		ffprobe:        cfg.FFprobe,
		timeout:        timeout,
		maxVideoPixels: maxVideoPixels,
		slots:          NewSlots(workers, cfg.PerUser),
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
	Pixels     int64
	Duration   time.Duration
	HasVideo   bool
	HasAudio   bool
	StillImage bool

	VideoCodec string
	AudioCodec string
	PixFmt     string
	FrameRate  float64
	BitRate    int64
}

const (
	maxVideoShortSide = 1080
	maxVideoFPS       = 30
	maxRemuxBitRate   = 4_500_000
)

func (p Probe) Remuxable() bool {
	long, short := max(p.Width, p.Height), min(p.Width, p.Height)
	return strings.Contains(p.Format, "mp4") &&
		p.VideoCodec == "h264" && p.PixFmt == "yuv420p" &&
		(!p.HasAudio || p.AudioCodec == "aac") &&
		long <= MaxVideoSide && short <= maxVideoShortSide &&
		p.FrameRate > 0 && p.FrameRate <= maxVideoFPS+0.5 &&
		p.BitRate > 0 && p.BitRate <= maxRemuxBitRate
}

type probeOutput struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		PixFmt      string `json:"pix_fmt"`
		FrameRate   string `json:"avg_frame_rate"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
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
	if bitRate, err := strconv.ParseInt(parsed.Format.BitRate, 10, 64); err == nil {
		probe.BitRate = bitRate
	}
	if seconds, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil && !math.IsNaN(seconds) && seconds > 0 {
		probe.Duration = time.Duration(seconds * float64(time.Second))
	}
	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			probe.Pixels = max(probe.Pixels, int64(s.Width)*int64(s.Height))
			if s.Disposition.AttachedPic == 1 || probe.HasVideo {
				continue
			}
			probe.HasVideo = true
			probe.Width, probe.Height = s.Width, s.Height
			probe.StillImage = isStillCodec(s.CodecName)
			probe.VideoCodec, probe.PixFmt = s.CodecName, s.PixFmt
			probe.FrameRate = parseRate(s.FrameRate)
		case "audio":
			if !probe.HasAudio {
				probe.AudioCodec = s.CodecName
			}
			probe.HasAudio = true
		}
	}
	if probe.Pixels > t.maxVideoPixels {
		return Probe{}, fmt.Errorf("%w: a %d pixel picture", ErrTooLarge, probe.Pixels)
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
		"-fpsmax", strconv.Itoa(maxVideoFPS),
		"-c:v", "libx264", "-preset", "medium", "-crf", "25",
		"-maxrate", "4M", "-bufsize", "8M",
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

func (t *Tools) RemuxVideo(ctx context.Context, in, out string) error {
	_, err := t.run(ctx, t.ffmpeg,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-protocol_whitelist", "file",
		"-format_whitelist", VideoDemuxers,
		"-i", "file:"+in,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-sn", "-dn",
		"-c", "copy",
		"-map_metadata", "-1", "-map_chapters", "-1",
		"-movflags", "+faststart",
		"-f", "mp4", "file:"+out,
	)
	if err != nil {
		return fmt.Errorf("%w: remux video: %w", ErrUnsupported, err)
	}
	return nil
}

func parseRate(raw string) float64 {
	num, den, found := strings.Cut(raw, "/")
	n, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	if !found {
		return n
	}
	d, err := strconv.ParseFloat(den, 64)
	if err != nil || d == 0 {
		return 0
	}
	return n / d
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
	release, err := t.slots.Acquire(ctx, OwnerOf(ctx))
	if err != nil {
		return nil, err
	}
	defer release()

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
