package logger

import (
	"log/slog"
	"testing"
)

func TestLoggerInit(t *testing.T) {
	tests := []struct {
		level string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo},
	}

	for _, tt := range tests {
		Init(tt.level)
		if Log == nil {
			t.Fatalf("Log is nil after Init(%q)", tt.level)
		}
	}
}
