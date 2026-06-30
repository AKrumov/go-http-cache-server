// Package logging provides structured logging setup using log/slog.
package logging

import (
	"log/slog"
	"os"
)

// Config holds logging configuration.
type Config struct {
	Format string // "json" or "text"
	Level  string // "debug", "info", "warn", "error"
}

// DefaultConfig returns the default logging configuration.
func DefaultConfig() Config {
	return Config{
		Format: envOrDefault("LOG_FORMAT", "text"),
		Level:  envOrDefault("LOG_LEVEL", "info"),
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Setup initializes the global slog logger with the given configuration.
func Setup(cfg Config) {
	level := parseLevel(cfg.Level)
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}

	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	slog.SetDefault(slog.New(handler))
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
