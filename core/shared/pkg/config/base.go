package config

import (
	"log/slog"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/logger"
)

type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvProduction  Environment = "production"
)

func (e Environment) IsProduction() bool {
	return e == EnvProduction
}

type Base struct {
	Service string

	Environment Environment

	HTTPAddr string

	MetricsAddr string

	LogLevel slog.Level

	LogFormat logger.Format

	ShutdownTimeout time.Duration
}

func LoadBase(l *Loader, service string) Base {
	env := Environment(l.OneOf("APP_ENV", string(EnvDevelopment),
		string(EnvDevelopment), string(EnvTest), string(EnvProduction)))

	level, err := logger.ParseLevel(l.StringDefault("LOG_LEVEL", "info"))
	if err != nil {
		l.fail("LOG_LEVEL", err)
	}

	format, err := logger.ParseFormat(l.StringDefault("LOG_FORMAT", "json"))
	if err != nil {
		l.fail("LOG_FORMAT", err)
	}

	return Base{
		Service:         service,
		Environment:     env,
		HTTPAddr:        l.StringDefault("HTTP_ADDR", ":8080"),
		MetricsAddr:     l.StringDefault("METRICS_ADDR", ":9090"),
		LogLevel:        level,
		LogFormat:       format,
		ShutdownTimeout: l.DurationDefault("SHUTDOWN_TIMEOUT", 15*time.Second),
	}
}

func (b Base) Logger() *slog.Logger {
	return logger.New(logger.Options{
		Service: b.Service,
		Level:   b.LogLevel,
		Format:  b.LogFormat,
	})
}
