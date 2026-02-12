package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

const requestContextKey contextKey = "request_context"

// RequestContext holds request-scoped identifiers and metadata for observability
type RequestContext struct {
	// Request Identifiers
	RequestID string // From X-Request-ID header OR generated UUID
	TraceID   string // From OTel span context
	SpanID    string // From OTel span context

	// Business Identifiers
	PodID     string // Pulse pod ID
	Namespace string // K8s namespace

	// User/Auth
	UserID    string // From JWT claims (sub)
	UserEmail string // From JWT claims (email) - primary label for metrics
	TenantID  string // From JWT claims or derived from email domain

	// Network
	ClientIP string // From X-Forwarded-For / X-Real-IP / RemoteAddr
	ServerIP string // Server's own IP

	// Service
	ServiceName    string // From OTEL_SERVICE_NAME env
	ServerInstance string // Unique server instance ID (hostname or pod name)
	Environment    string // Deployment environment (development, production, staging)
}

// SetRequestContext stores the RequestContext in the context
func SetRequestContext(ctx context.Context, reqCtx *RequestContext) context.Context {
	return context.WithValue(ctx, requestContextKey, reqCtx)
}

// GetRequestContext retrieves the RequestContext from the context
func GetRequestContext(ctx context.Context) *RequestContext {
	if reqCtx, ok := ctx.Value(requestContextKey).(*RequestContext); ok {
		return reqCtx
	}
	return nil
}

// ExtractTraceContext extracts trace_id and span_id from OTel span in context
func ExtractTraceContext(ctx context.Context) (traceID, spanID string) {
	span := trace.SpanFromContext(ctx)
	spanContext := span.SpanContext()

	if spanContext.IsValid() {
		traceID = spanContext.TraceID().String()
		spanID = spanContext.SpanID().String()
	}

	return traceID, spanID
}

// UpdateRequestContext updates fields in the existing RequestContext
func UpdateRequestContext(ctx context.Context, updateFn func(*RequestContext)) {
	if reqCtx := GetRequestContext(ctx); reqCtx != nil {
		updateFn(reqCtx)
	}
}
