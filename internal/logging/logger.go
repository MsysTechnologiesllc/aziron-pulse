package logging

import (
	"context"

	"go.uber.org/zap"
)

// contextKey for logger
type loggerKey struct{}

// FromContext retrieves a logger from context or returns a default logger
func FromContext(ctx context.Context) *zap.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*zap.Logger); ok {
		return logger
	}
	
	// Return default logger
	logger, _ := zap.NewProduction()
	return logger
}

// WithContext adds a logger to the context
func WithContext(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}
