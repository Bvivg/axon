package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	kafkago "github.com/segmentio/kafka-go"
)

type printer struct {
	log   *slog.Logger
	level slog.Level
}

func (p printer) Printf(format string, args ...any) {
	p.log.Log(context.Background(), p.level,
		strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func errorPrinter(log *slog.Logger) kafkago.Logger {
	return printer{log: log.With("component", "kafka.client"), level: slog.LevelError}
}

func debugPrinter(log *slog.Logger) kafkago.Logger {
	if !log.Enabled(context.Background(), slog.LevelDebug) {
		return nil
	}
	return printer{log: log.With("component", "kafka.client"), level: slog.LevelDebug}
}
