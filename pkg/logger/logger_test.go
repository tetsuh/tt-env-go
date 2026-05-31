package logger

import (
	"context"
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
		t.Run(tt.level, func(t *testing.T) {
			logger := initLogger(tt.level)
			if logger == nil {
				t.Fatalf("initLogger(%q) returned nil", tt.level)
			}

			// Verify the logger's effective level by checking if it would log at the expected level
			if !logger.Enabled(context.Background(), tt.want) {
				t.Errorf("initLogger(%q) logger not enabled for level %v", tt.level, tt.want)
			}

			// Verify it's disabled for levels below the expected level (if not debug)
			if tt.want > slog.LevelDebug {
				belowLevel := tt.want - 1
				if logger.Enabled(context.Background(), belowLevel) {
					t.Errorf("initLogger(%q) logger should not be enabled for level %v", tt.level, belowLevel)
				}
			}
		})
	}
}
