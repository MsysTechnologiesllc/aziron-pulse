package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// SpanContext holds trace and span IDs for database storage
type SpanContext struct {
	TraceID string
	SpanID  string
}

// ExtractSpanContextForDB extracts trace and span IDs from the current span for database storage
func ExtractSpanContextForDB(ctx context.Context) *SpanContext {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return nil
	}

	return &SpanContext{
		TraceID: spanCtx.TraceID().String(),
		SpanID:  spanCtx.SpanID().String(),
	}
}

// ParseSpanContext parses stored trace and span IDs back into a trace.SpanContext
func ParseSpanContext(traceID, spanID string) (trace.SpanContext, error) {
	tid, err := trace.TraceIDFromHex(traceID)
	if err != nil {
		return trace.SpanContext{}, err
	}

	sid, err := trace.SpanIDFromHex(spanID)
	if err != nil {
		return trace.SpanContext{}, err
	}

	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid,
		SpanID:  sid,
	}), nil
}
