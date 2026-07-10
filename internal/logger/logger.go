package logger

import (
	"log/slog"
	"os"
)

// Init initializes the global slog logger with JSON format.
func Init() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

// WithCorrelation adds a correlation ID to a logger.
func WithCorrelation(id string) *slog.Logger {
	return slog.With("correlation_id", id)
}

// WithProject adds a project ID to a logger.
func WithProject(id string) *slog.Logger {
	return slog.With("project_id", id)
}

// WithTask adds a task ID to a logger.
func WithTask(id string) *slog.Logger {
	return slog.With("task_id", id)
}
