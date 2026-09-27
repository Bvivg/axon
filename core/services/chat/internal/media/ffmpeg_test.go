package media_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bvivg/axon/core/services/chat/internal/media"
)

func tools(t *testing.T) *media.Tools {
	t.Helper()

	tools := media.NewTools(media.ToolsConfig{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Workers: 2, Timeout: time.Minute})
	if err := tools.Check(); err != nil {
		t.Skipf("ffmpeg is not installed here: %v", err)
	}
	return tools
}

func generate(t *testing.T, name string, args ...string) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), name)
	cmd := exec.CommandContext(t.Context(), "ffmpeg", append(append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...), out)...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate %s: %v: %s", name, err, msg)
	}
	return out
}

func generateOrSkip(t *testing.T, name string, args ...string) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), name)
	cmd := exec.CommandContext(t.Context(), "ffmpeg", append(append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...), out)...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("this ffmpeg cannot build %s: %v: %s", name, err, msg)
	}
	return out
}

func describe(t *testing.T, path string) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "ffprobe", "-v", "error", "-show_format", "-show_streams", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe %s: %v: %s", path, err, out)
	}
	return string(out)
}

func TestVideoIsReencodedWithoutItsMetadata(t *testing.T) {
	tl := tools(t)

	in := generate(t, "in.mov",
		"-f", "lavfi", "-i", "testsrc=size=640x360:rate=25:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-metadata", "title=holiday-secret",
		"-metadata", "location=+55.7558+037.6173/",
		"-c:v", "mpeg4", "-c:a", "aac", "-shortest",
	)
	out := filepath.Join(t.TempDir(), "out.mp4")

	if err := tl.TranscodeVideo(t.Context(), in, out); err != nil {
		t.Fatalf("TranscodeVideo: %v", err)
	}

	probe, err := tl.Probe(t.Context(), out, media.MP4Demuxer)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !probe.HasVideo || !probe.HasAudio {
		t.Errorf("probe = %+v, want video and audio", probe)
	}
	if probe.Width != 640 || probe.Height != 360 {
		t.Errorf("size = %dx%d, want 640x360", probe.Width, probe.Height)
	}
	if probe.Duration < 1900*time.Millisecond || probe.Duration > 2100*time.Millisecond {
		t.Errorf("duration = %v, want about 2s", probe.Duration)
	}

	details := describe(t, out)
	for _, leak := range []string{"holiday-secret", "55.7558", "TAG:location"} {
		if strings.Contains(details, leak) {
			t.Errorf("the output still mentions %q", leak)
		}
	}
	if !strings.Contains(details, "codec_name=h264") {
		t.Error("the output is not H.264")
	}

	poster := filepath.Join(t.TempDir(), "poster.jpg")
	if err := tl.Poster(t.Context(), out, poster, 500*time.Millisecond); err != nil {
		t.Fatalf("Poster: %v", err)
	}
	if info, err := os.Stat(poster); err != nil || info.Size() == 0 {
		t.Errorf("poster was not written: %v", err)
	}
}

func TestLargeVideoIsScaledDown(t *testing.T) {
	tl := tools(t)

	in := generate(t, "big.mp4",
		"-f", "lavfi", "-i", "testsrc=size=2560x1440:rate=10:duration=1",
		"-c:v", "mpeg4",
	)
	out := filepath.Join(t.TempDir(), "out.mp4")

	if err := tl.TranscodeVideo(t.Context(), in, out); err != nil {
		t.Fatalf("TranscodeVideo: %v", err)
	}
	probe, err := tl.Probe(t.Context(), out, media.MP4Demuxer)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Width != media.MaxVideoSide || probe.Height != 1080 {
		t.Errorf("size = %dx%d, want %dx1080", probe.Width, probe.Height, media.MaxVideoSide)
	}
	if probe.HasAudio {
		t.Error("a silent video came out with an audio track")
	}
}

func TestVoiceBecomesAACThatPlaysEverywhere(t *testing.T) {
	tl := tools(t)

	in := generate(t, "voice.webm",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=1.5",
		"-c:a", "libopus",
	)
	out := filepath.Join(t.TempDir(), "voice.m4a")

	if err := tl.TranscodeVoice(t.Context(), in, out); err != nil {
		t.Fatalf("TranscodeVoice: %v", err)
	}
	probe, err := tl.Probe(t.Context(), out, media.MP4Demuxer)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.HasVideo || !probe.HasAudio {
		t.Errorf("probe = %+v, want audio only", probe)
	}
	if probe.Duration < 1400*time.Millisecond || probe.Duration > 1700*time.Millisecond {
		t.Errorf("duration = %v, want about 1.5s", probe.Duration)
	}
	if !strings.Contains(describe(t, out), "codec_name=aac") {
		t.Error("the voice message is not AAC")
	}
}

func TestAPlaylistCannotPullInOtherFiles(t *testing.T) {
	tl := tools(t)

	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("DATABASE_PASSWORD=hunter2"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	playlist := filepath.Join(dir, "clip.mp4")
	body := "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1.0,\nfile://" + secret + "\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(playlist, []byte(body), 0o600); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	_, err := tl.Probe(t.Context(), playlist, media.VideoDemuxers)
	if !errors.Is(err, media.ErrUnsupported) {
		t.Fatalf("Probe = %v, want ErrUnsupported", err)
	}

	err = tl.TranscodeVideo(t.Context(), playlist, filepath.Join(dir, "out.mp4"))
	if !errors.Is(err, media.ErrUnsupported) {
		t.Fatalf("TranscodeVideo = %v, want ErrUnsupported", err)
	}
}

func TestAPortraitPhoneVideoComesOutUpright(t *testing.T) {
	tl := tools(t)

	plain := generate(t, "plain.mp4",
		"-f", "lavfi", "-i", "testsrc=size=640x360:rate=10:duration=1",
		"-c:v", "mpeg4",
	)
	rotated := generateOrSkip(t, "rotated.mp4", "-display_rotation", "90", "-i", plain, "-c", "copy")
	out := filepath.Join(t.TempDir(), "out.mp4")

	if err := tl.TranscodeVideo(t.Context(), rotated, out); err != nil {
		t.Fatalf("TranscodeVideo: %v", err)
	}
	probe, err := tl.Probe(t.Context(), out, media.MP4Demuxer)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Width != 360 || probe.Height != 640 {
		t.Errorf("size = %dx%d, want 360x640", probe.Width, probe.Height)
	}
	if strings.Contains(describe(t, out), "rotation=") {
		t.Error("the output still asks the player to rotate it")
	}
}
