package middleware

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.opentelemetry.io/otel/trace"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
)

// RequestContextMiddleware enriches request context with telemetry data
type RequestContextMiddleware struct{}

// NewRequestContextMiddleware creates a new request context middleware
func NewRequestContextMiddleware() *RequestContextMiddleware {
	return &RequestContextMiddleware{}
}

// Enrich adds telemetry context from the request
func (m *RequestContextMiddleware) Enrich(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract OpenTelemetry span context
		spanCtx := trace.SpanContextFromContext(ctx)

		// Extract user information from auth middleware context
		userID, _ := ctx.Value(userIDKey).(uuid.UUID)
		tenantID, _ := ctx.Value(tenantIDKey).(uuid.UUID)
		userEmail, _ := ctx.Value(userEmailKey).(string)

		// Extract pod_id from URL if present (for proxy requests)
		vars := mux.Vars(r)
		podID := vars["pulse_id"]

		// Build RequestContext
		reqCtx := telemetry.RequestContext{
			TraceID:     spanCtx.TraceID().String(),
			SpanID:      spanCtx.SpanID().String(),
			RequestID:   r.Header.Get("X-Request-ID"),
			PodID:       podID,
			UserID:      userID.String(),
			UserEmail:   userEmail,
			TenantID:    tenantID.String(),
			Namespace:   "", // Will be populated by K8s operations
			ClientIP:    getClientIP(r),
			ServiceName: "aziron-pulse",
		}

		// Set RequestContext in context
		ctx = telemetry.SetRequestContext(ctx, &reqCtx)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
