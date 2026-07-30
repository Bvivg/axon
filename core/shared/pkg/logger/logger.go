// Package logger builds the structured logger every Axon service uses.
//
// Output is JSON by default with a fixed set of mandatory fields — timestamp,
// level, service and (when the context carries one) correlation_id — so that
// log lines from any service are queryable the same way. Correlation IDs are
// pulled off context.Context automatically: callers use logger.InfoContext and
// friends, and never have to thread the ID through by hand.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

// Mandatory field names. They are constants because dashboards and log
// queries depend on the exact spelling.
const (
	FieldTimestamp     = "timestamp"
	FieldLevel         = "level"
	FieldService       = "service"
	FieldCorrelationID = "correlation_id"
)

// Format selects the handler used for output.
type Format string

const (
	// FormatJSON is the production format and the default.
	FormatJSON Format = "json"
	// FormatText is human-readable output for local debugging only.
	FormatText Format = "text"
)

// Options configures New. Service is the only required field.
type Options struct {
	// Service names the emitting service and is attached to every line.
	Service string
	// Level is the minimum level that gets emitted.
	Level slog.Level
	// Format selects JSON (default) or text output.
	Format Format
	// Output defaults to os.Stdout. Logs are a stream, not a file: shipping
	// them off the container is the platform's job, not ours.
	Output io.Writer
	// AddSource includes file:line. Useful for errors, noisy otherwise.
	AddSource bool
}

// New returns a logger that stamps every record with the mandatory fields.
func New(opts Options) *slog.Logger {
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}

	handlerOpts := &slog.HandlerOptions{
		Level:       opts.Level,
		AddSource:   opts.AddSource,
		ReplaceAttr: replaceAttr,
	}

	var handler slog.Handler
	if opts.Format == FormatText {
		handler = slog.NewTextHandler(out, handlerOpts)
	} else {
		handler = slog.NewJSONHandler(out, handlerOpts)
	}

	handler = handler.WithAttrs([]slog.Attr{slog.String(FieldService, opts.Service)})

	return slog.New(contextHandler{Handler: handler})
}

// Discard returns a logger that drops everything. Tests that exercise code
// paths which log use it to keep output clean.
func Discard() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// replaceAttr renames slog's default "time" key to "timestamp". The rename is
// scoped to the top level so a user-supplied attribute named "time" inside a
// group is left alone.
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		a.Key = FieldTimestamp
	}
	return a
}

// contextHandler decorates records with values that live on the context
// rather than on the call site.
type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := correlation.FromContext(ctx); id != "" {
		r.AddAttrs(slog.String(FieldCorrelationID, id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}

// ParseLevel maps a configuration string onto an slog.Level. It is
// deliberately strict: an unrecognised level is a configuration error, not a
// reason to silently fall back to info.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want debug, info, warn or error)", s)
	}
}

// ParseFormat maps a configuration string onto a Format.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json", "":
		return FormatJSON, nil
	case "text":
		return FormatText, nil
	default:
		return "", fmt.Errorf("unknown log format %q (want json or text)", s)
	}
}
