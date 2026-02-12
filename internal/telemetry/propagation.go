package telemetry

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// InjectTraceHeaders injects W3C trace context and custom business headers into HTTP request
func InjectTraceHeaders(ctx context.Context, req *http.Request) {
	// Inject W3C Trace Context (traceparent, tracestate)
	carrier := propagation.HeaderCarrier(req.Header)
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	// Inject custom business context headers
	if reqCtx := GetRequestContext(ctx); reqCtx != nil {
		if reqCtx.UserID != "" {
			req.Header.Set("X-User-ID", reqCtx.UserID)
		}
		if reqCtx.UserEmail != "" {
			req.Header.Set("X-User-Email", reqCtx.UserEmail)
		}
		if reqCtx.TenantID != "" {
			req.Header.Set("X-Tenant-ID", reqCtx.TenantID)
		}
		if reqCtx.RequestID != "" {
			req.Header.Set("X-Request-ID", reqCtx.RequestID)
		}
		if reqCtx.PodID != "" {
			req.Header.Set("X-Pod-ID", reqCtx.PodID)
		}
	}
}

// ExtractTraceHeaders extracts W3C trace context and custom headers from HTTP request
func ExtractTraceHeaders(req *http.Request) context.Context {
	// Extract W3C Trace Context
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.HeaderCarrier(req.Header))

	// Extract custom business context headers
	reqCtx := &RequestContext{
		UserID:    req.Header.Get("X-User-ID"),
		UserEmail: req.Header.Get("X-User-Email"),
		TenantID:  req.Header.Get("X-Tenant-ID"),
		RequestID: req.Header.Get("X-Request-ID"),
		PodID:     req.Header.Get("X-Pod-ID"),
	}

	// Store in context
	ctx = SetRequestContext(ctx, reqCtx)

	return ctx
}
