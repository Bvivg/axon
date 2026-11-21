package config

import (
	"time"

	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
	"github.com/bvivg/axon/core/shared/pkg/redis"
)

const ServiceName = "chat"

type Config struct {
	config.Base

	Postgres    postgres.Config
	Redis       redis.Config
	JWT         JWTConfig
	Auth        AuthConfig
	Kafka       KafkaConfig
	Attachments AttachmentConfig
	Media       MediaConfig
	Uploads     UploadsConfig
	Push        PushConfig
	Calls       CallsConfig
	Fanout      FanoutConfig
	Throttle    ThrottleConfig
}

type Rate struct {
	PerMinute int
	Burst     int
}

type ThrottleConfig struct {
	Send      Rate
	Typing    Rate
	Subscribe Rate
	Push      Rate
}

type FanoutConfig struct {
	Shards int
	MaxLen int
	Block  time.Duration
}

type CallsConfig struct {
	MigrationEnabled    bool
	DisableNewMigration bool
	SFUEnabled          bool
	DisableNewSFU       bool
	PublicMediaURL      string
	MediaProbe          bool
	MediaAPIKey         string
	MediaURL            string
	MediaAPISecret      config.Secret
	InternalKey         config.Secret
	Enabled             bool
	TURNURLs            []string
	TURNSecret          config.Secret
}

type PushConfig struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}

type UploadsConfig struct {
	MaxBytes   int64
	PartBytes  int64
	Workers    int
	DailyBytes int64
}

type MediaConfig struct {
	FFmpeg  string
	FFprobe string

	Workers        int
	PerUser        int
	ImageWorkers   int
	Timeout        time.Duration
	MaxVideoPixels int64

	Images ImageLimitsConfig
}

type ImageLimitsConfig struct {
	MaxPixels          int64
	MaxFrames          int
	MaxAnimationPixels int64
	MaxAnimationScreen int64
}

type KafkaConfig struct {
	Brokers []string
}

type AttachmentConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
	PublicURL string

	SweepInterval time.Duration
	OrphanAfter   time.Duration
}

type JWTConfig struct {
	JWKSURL string

	JWKSRefreshInterval time.Duration

	Issuer   string
	Audience string
}

type AuthConfig struct {
	ServiceURL string

	Timeout time.Duration
}

func Load() (Config, error) {
	l := config.NewLoader()

	cfg := Config{
		Base:     config.LoadBase(l, ServiceName),
		Postgres: postgres.LoadConfig(l),
		Redis:    redis.LoadConfig(l),
		Calls:    CallsConfig{MigrationEnabled: l.Bool("CALLS_MIGRATION_ENABLED", false), DisableNewMigration: l.Bool("CALLS_MIGRATION_DISABLE_NEW", false), Enabled: l.Bool("CALLS_ENABLED", false), TURNURLs: l.StringSlice("CALLS_TURN_URLS", nil), TURNSecret: l.SecretDefault("CALLS_TURN_SECRET", ""), MediaProbe: l.Bool("CALLS_MEDIA_ADMISSION_PROBE", false), MediaAPIKey: l.StringDefault("LIVEKIT_API_KEY", ""), MediaAPISecret: l.SecretDefault("LIVEKIT_API_SECRET", ""), InternalKey: l.SecretDefault("CALLS_INTERNAL_KEY", ""), MediaURL: l.StringDefault("LIVEKIT_API_URL", ""), SFUEnabled: l.Bool("CALLS_SFU_ENABLED", false), DisableNewSFU: l.Bool("CALLS_SFU_DISABLE_NEW", false), PublicMediaURL: l.StringDefault("LIVEKIT_PUBLIC_URL", "/livekit")},
		JWT: JWTConfig{
			JWKSURL:             l.StringDefault("JWKS_URL", "http://auth:9091/.well-known/jwks.json"),
			JWKSRefreshInterval: l.DurationDefault("JWKS_REFRESH_INTERVAL", 15*time.Minute),
			Issuer:              l.StringDefault("JWT_ISSUER", "https://auth.axon.local"),
			Audience:            l.StringDefault("JWT_AUDIENCE", "axon"),
		},
		Auth: AuthConfig{
			ServiceURL: l.StringDefault("AUTH_SERVICE_URL", "http://auth:8081"),
			Timeout:    l.DurationDefault("AUTH_SERVICE_TIMEOUT", 3*time.Second),
		},
		Kafka: KafkaConfig{
			Brokers: l.StringSlice("KAFKA_BROKERS", nil),
		},
		Attachments: loadAttachments(l),
		Media: MediaConfig{
			FFmpeg:         l.StringDefault("FFMPEG_PATH", "ffmpeg"),
			FFprobe:        l.StringDefault("FFPROBE_PATH", "ffprobe"),
			Workers:        l.IntDefault("MEDIA_WORKERS", 2),
			PerUser:        l.IntDefault("MEDIA_WORKERS_PER_USER", 1),
			ImageWorkers:   l.IntDefault("MEDIA_IMAGE_WORKERS", 2),
			Timeout:        l.DurationDefault("MEDIA_TIMEOUT", 15*time.Minute),
			MaxVideoPixels: megapixels(l, "MEDIA_VIDEO_MAX_MP", 36),
			Images: ImageLimitsConfig{
				MaxPixels:          megapixels(l, "MEDIA_IMAGE_MAX_MP", 50),
				MaxFrames:          l.IntDefault("MEDIA_GIF_MAX_FRAMES", 1000),
				MaxAnimationPixels: megapixels(l, "MEDIA_GIF_MAX_MP", 300),
				MaxAnimationScreen: megapixels(l, "MEDIA_GIF_MAX_SCREEN_MP", 16),
			},
		},
		Push: PushConfig{
			PublicKey:  l.StringDefault("CHAT_VAPID_PUBLIC_KEY", ""),
			PrivateKey: l.SecretDefault("CHAT_VAPID_PRIVATE_KEY", "").Reveal(),
			Subject:    l.StringDefault("CHAT_VAPID_SUBJECT", "mailto:push@axon.test"),
		},
		Uploads: UploadsConfig{
			MaxBytes:  int64(l.IntDefault("CHAT_MAX_UPLOAD_MB", 200)) << 20,
			PartBytes: int64(l.IntDefault("CHAT_UPLOAD_PART_MB", 10)) << 20,
			Workers:   l.IntDefault("CHAT_UPLOAD_WORKERS", 2),

			DailyBytes: int64(l.IntDefault("CHAT_UPLOAD_DAILY_MB", 2048)) << 20,
		},
		Fanout: FanoutConfig{
			Shards: l.IntDefault("CHAT_FANOUT_SHARDS", 16),
			MaxLen: l.IntDefault("CHAT_FANOUT_MAXLEN", 10000),
			Block:  l.DurationDefault("CHAT_FANOUT_BLOCK", time.Second),
		},
		Throttle: ThrottleConfig{
			Send:      Rate{PerMinute: l.IntDefault("CHAT_WS_SEND_PER_MINUTE", 300), Burst: l.IntDefault("CHAT_WS_SEND_BURST", 20)},
			Typing:    Rate{PerMinute: l.IntDefault("CHAT_WS_TYPING_PER_MINUTE", 120), Burst: l.IntDefault("CHAT_WS_TYPING_BURST", 5)},
			Subscribe: Rate{PerMinute: l.IntDefault("CHAT_WS_SUBSCRIBE_PER_MINUTE", 600), Burst: l.IntDefault("CHAT_WS_SUBSCRIBE_BURST", 500)},
			Push:      Rate{PerMinute: l.IntDefault("CHAT_PUSH_PER_SENDER_PER_MINUTE", 6), Burst: l.IntDefault("CHAT_PUSH_PER_SENDER_BURST", 5)},
		},
	}

	if cfg.Postgres.SearchPath == "" {
		cfg.Postgres.SearchPath = ServiceName
	}

	if err := l.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func megapixels(l *config.Loader, key string, def int) int64 {
	return int64(l.IntDefault(key, def)) * 1_000_000
}

func loadAttachments(l *config.Loader) AttachmentConfig {
	return AttachmentConfig{
		Endpoint:  l.StringDefault("ATTACHMENTS_ENDPOINT", "minio:9000"),
		AccessKey: l.StringDefault("MINIO_ROOT_USER", "axon"),
		SecretKey: l.SecretDefault("MINIO_ROOT_PASSWORD", "axon12345").Reveal(),
		Bucket:    l.StringDefault("ATTACHMENTS_BUCKET", "chat-attachments"),
		UseSSL:    l.Bool("ATTACHMENTS_USE_SSL", false),
		PublicURL: l.StringDefault("ATTACHMENTS_PUBLIC_URL", "http://localhost:59000"),

		SweepInterval: l.DurationDefault("ATTACHMENTS_SWEEP_INTERVAL", time.Hour),
		OrphanAfter:   l.DurationDefault("ATTACHMENTS_ORPHAN_AFTER", 24*time.Hour),
	}
}
