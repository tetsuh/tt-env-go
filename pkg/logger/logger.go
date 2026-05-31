package logger

import (
	"log/slog"
	"os"
	"sync"
)

var (
	Log      = slog.New(slog.NewTextHandler(os.Stderr, nil))
	initOnce sync.Once
)

// Init initializes the global logger with a specific level.
// levelStr can be "debug", "info", "warn", "error".
func Init(levelStr string) {
	initOnce.Do(func() {
		var level slog.Level
		switch levelStr {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		default:
			level = slog.LevelInfo
		}

		opts := &slog.HandlerOptions{
			Level: level,
		}

		// Use structured text logging on Stderr for CLI friendliness
		handler := slog.NewTextHandler(os.Stderr, opts)
		Log = slog.New(handler)
		slog.SetDefault(Log)
	})
}
