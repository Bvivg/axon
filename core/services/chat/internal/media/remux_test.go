package media

import "testing"

func TestRemuxableOnlyForAlreadyThriftyMP4(t *testing.T) {
	good := Probe{
		Format: "mov,mp4,m4a,3gp,3g2,mj2", Width: 1920, Height: 1080, HasVideo: true, HasAudio: true,
		VideoCodec: "h264", AudioCodec: "aac", PixFmt: "yuv420p", FrameRate: 29.97, BitRate: 3_000_000,
	}

	tests := map[string]struct {
		edit func(*Probe)
		want bool
	}{
		"compatible phone clip": {edit: func(*Probe) {}, want: true},
		"no audio track":        {edit: func(p *Probe) { p.HasAudio, p.AudioCodec = false, "" }, want: true},
		"hevc":                  {edit: func(p *Probe) { p.VideoCodec = "hevc" }, want: false},
		"10-bit colour":         {edit: func(p *Probe) { p.PixFmt = "yuv420p10le" }, want: false},
		"opus audio":            {edit: func(p *Probe) { p.AudioCodec = "opus" }, want: false},
		"60 frames per second":  {edit: func(p *Probe) { p.FrameRate = 59.94 }, want: false},
		"4k":                    {edit: func(p *Probe) { p.Width, p.Height = 3840, 2160 }, want: false},
		"portrait 1080p":        {edit: func(p *Probe) { p.Width, p.Height = 1080, 1920 }, want: true},
		"too many bits":         {edit: func(p *Probe) { p.BitRate = 8_000_000 }, want: false},
		"unknown bit rate":      {edit: func(p *Probe) { p.BitRate = 0 }, want: false},
		"webm container":        {edit: func(p *Probe) { p.Format = "matroska,webm" }, want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			p := good
			tt.edit(&p)
			if got := p.Remuxable(); got != tt.want {
				t.Fatalf("Remuxable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseRate(t *testing.T) {
	for raw, want := range map[string]float64{"30000/1001": 30000.0 / 1001, "25/1": 25, "24": 24, "0/0": 0, "x": 0} {
		if got := parseRate(raw); got != want {
			t.Errorf("parseRate(%q) = %v, want %v", raw, got, want)
		}
	}
}
