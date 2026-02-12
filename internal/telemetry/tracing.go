package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// AdaptiveSampler implements adaptive sampling based on trace attributes
// - 100% sampling for errors
// - 10% sampling for successful requests
// - 1% sampling for high-volume operations (K8s watch events)
type AdaptiveSampler struct {
	errorSampler       sdktrace.Sampler
	successSampler     sdktrace.Sampler
	highVolumeSampler  sdktrace.Sampler
	parentBasedSampler sdktrace.Sampler
}

// NewAdaptiveSampler creates a new adaptive sampler with configurable rates
func NewAdaptiveSampler() sdktrace.Sampler {
	errorRate := getEnvFloat("TRACE_SAMPLE_ERROR_RATE", 1.0)             // Default 100%
	successRate := getEnvFloat("TRACE_SAMPLE_SUCCESS_RATE", 0.10)        // Default 10%
	highVolumeRate := getEnvFloat("TRACE_SAMPLE_HIGH_VOLUME_RATE", 0.01) // Default 1%

	return &AdaptiveSampler{
		errorSampler:      sdktrace.TraceIDRatioBased(errorRate),
		successSampler:    sdktrace.TraceIDRatioBased(successRate),
		highVolumeSampler: sdktrace.TraceIDRatioBased(highVolumeRate),
		parentBasedSampler: sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(successRate),
		),
	}
}

// ShouldSample implements the Sampler interface
func (s *AdaptiveSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	parentSpanContext := trace.SpanContextFromContext(parameters.ParentContext)

	// If parent is sampled, always sample (maintains trace consistency)
	if parentSpanContext.IsSampled() {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Tracestate: parentSpanContext.TraceState(),
		}
	}

	// Check for high-volume operations first (K8s watch events)
	spanName := parameters.Name
	if isHighVolumeOperation(spanName) {
		return s.highVolumeSampler.ShouldSample(parameters)
	}

	// Check attributes for error indicators
	hasError := false
	for _, attr := range parameters.Attributes {
		key := string(attr.Key)
		value := attr.Value.AsString()

		if key == "error" && value == "true" {
			hasError = true
			break
		}
		if key == "http.status_code" {
			if strings.HasPrefix(value, "5") || strings.HasPrefix(value, "4") {
				hasError = true
				break
			}
		}
		if key == "status" && (value == "error" || value == "failed") {
			hasError = true
			break
		}
	}

	// 100% sampling for errors
	if hasError {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Tracestate: parentSpanContext.TraceState(),
			Attributes: []attribute.KeyValue{
				attribute.String("sampling.reason", "error"),
				attribute.Float64("sampling.rate", 1.0),
			},
		}
	}

	// 10% sampling for successful requests
	result := s.successSampler.ShouldSample(parameters)
	if result.Decision == sdktrace.RecordAndSample {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Tracestate: result.Tracestate,
			Attributes: append(result.Attributes,
				attribute.String("sampling.reason", "success"),
				attribute.Float64("sampling.rate", 0.10),
			),
		}
	}

	return sdktrace.SamplingResult{
		Decision:   sdktrace.Drop,
		Tracestate: parentSpanContext.TraceState(),
	}
}

// Description returns a description of the sampler
func (s *AdaptiveSampler) Description() string {
	return "AdaptiveSampler{error=100%, success=10%, high_volume=1%}"
}

// isHighVolumeOperation checks if the span name indicates a high-volume operation
func isHighVolumeOperation(spanName string) bool {
	highVolumePatterns := []string{
		"k8s.watch.pod.status", // Pod status updates are frequent
		"k8s.watch.metrics",    // Metrics watch events
		"k8s.list.pods",        // Pod list operations
		"health_check",         // Health checks
		"heartbeat",            // Heartbeat pings
		"metrics.collect",      // Metrics collection
		"cadvisor.scrape",      // cAdvisor metric scraping
		"network.collect",      // Network stats collection
	}

	lowerName := strings.ToLower(spanName)
	for _, pattern := range highVolumePatterns {
		if strings.Contains(lowerName, pattern) {
			return true
		}
	}
	return false
}

// InitTracer initializes the OpenTelemetry TracerProvider with OTLP HTTP exporter
func InitTracer(ctx context.Context, serviceName, otlpEndpoint, environment string) (*sdktrace.TracerProvider, error) {
	// Create OTLP HTTP exporter to Tempo
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(otlpEndpoint),
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithTimeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Create resource with service metadata
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
			semconv.DeploymentEnvironmentKey.String(environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create TracerProvider with batch span processor and adaptive sampler
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(NewAdaptiveSampler()),
	)

	// Set global TracerProvider
	otel.SetTracerProvider(tp)

	// Set global propagator to W3C Trace Context
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}
