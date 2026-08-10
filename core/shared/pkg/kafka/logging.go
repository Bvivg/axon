package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	kafkago "github.com/segmentio/kafka-go"
)

// printer adapts the client library's Printf-style logger onto slog.
//
// Without it the library writes unstructured lines straight to stderr, which
// rules/observability.md rules out: every line a service emits has to carry
// service, level and timestamp in the same shape as the rest.
type printer struct {
	log   *slog.Logger
	level slog.Level
}

// Printf implements kafkago.Logger.
func (p printer) Printf(format string, args ...any) {
	p.log.Log(context.Background(), p.level,
		strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// errorPrinter routes the library's failures to the error level.
func errorPrinter(log *slog.Logger) kafkago.Logger {
	return printer{log: log.With("component", "kafka.client"), level: slog.LevelError}
}

// debugPrinter routes the library's chatter to the debug level, and only when
// the service is actually running at debug — the client narrates every fetch
// and every heartbeat, which is useful exactly once, while debugging.
func debugPrinter(log *slog.Logger) kafkago.Logger {
	if !log.Enabled(context.Background(), slog.LevelDebug) {
		return nil
	}
	return printer{log: log.With("component", "kafka.client"), level: slog.LevelDebug}
}
