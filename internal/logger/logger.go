package logger

import (
	"log/slog"
	"os"
	"strings"
)

// Init configura il logger globale slog.
// Impostando LOG_FORMAT=json produce output JSON; altrimenti usa formato testuale chiave-valore.
func Init(serviceName string) *slog.Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "json") {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	log := slog.New(handler).With(slog.String("service", serviceName))
	slog.SetDefault(log)
	return log
}