package config

import (
	"log/slog"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/logger"
)

// Environment names the deployment tier. It gates behaviour that must never be
// reachable in production, such as the fake OAuth provider.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvProduction  Environment = "production"
)

// IsProduction reports whether e is the production tier.
func (e Environment) IsProduction() bool {
	return e == EnvProduction
}

// Base is the configuration every Axon service needs. Services embed it and
// add their own fields.
type Base struct {
	// Service is the service's own name, stamped onto every log line.
	Service string
	// Environment is the deployment tier.
	Environment Environment
	// HTTPAddr is the listen address for the service's Connect/HTTP server.
	HTTPAddr string
	// MetricsAddr is the listen address for /metrics, /healthz and /readyz.
	// Keeping them off the public port means the operational surface is never
	// exposed through the gateway.
	MetricsAddr string
	// LogLevel is the minimum level that gets emitted.
	LogLevel slog.Level
	// LogFormat selects JSON or text output.
	LogFormat logger.Format
	// ShutdownTimeout bounds graceful shutdown before in-flight work is cut.
	ShutdownTimeout time.Duration
}

// LoadBase reads the common variables. It never returns an error of its own:
// problems accumulate on l, and the caller checks l.Err() once after loading
// its service-specific fields too, so a single startup reports every issue.
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

// Logger builds the service logger described by b.
func (b Base) Logger() *slog.Logger {
	return logger.New(logger.Options{
		Service: b.Service,
		Level:   b.LogLevel,
		Format:  b.LogFormat,
	})
}
