package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// PayloadSample represents a sampled payload with truncation metadata
type PayloadSample struct {
	Sample      string `json:"sample"`
	IsTruncated bool   `json:"is_truncated"`
	FullSize    int    `json:"full_size"`
}

// EstimatePayloadSize estimates the size of a payload in bytes
func EstimatePayloadSize(payload interface{}) int {
	if payload == nil {
		return 0
	}

	// Try to marshal to JSON to get accurate size
	data, err := json.Marshal(payload)
	if err != nil {
		// Fallback to string length
		return len(fmt.Sprintf("%v", payload))
	}

	return len(data)
}

// ShouldSamplePayload determines if large payload should be sampled (10% rate)
func ShouldSamplePayload() bool {
	sampleRate := getEnvFloat("TRACE_PAYLOAD_SAMPLING_RATE", 0.10) // Default 10%
	return shouldSample(sampleRate)
}

// SerializePayloadSample creates a sampled representation of large payloads
// Used for K8s objects and large tool results
func SerializePayloadSample(payload interface{}, maxSize int, maskPatterns []string) PayloadSample {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return PayloadSample{
			Sample:      fmt.Sprintf("error serializing: %v", err),
			IsTruncated: false,
			FullSize:    0,
		}
	}

	// Mask sensitive patterns (certificates, keys, etc.)
	maskedData := string(data)
	for _, pattern := range maskPatterns {
		maskedData = maskPattern(maskedData, pattern)
	}

	fullSize := len(maskedData)
	isTruncated := fullSize > maxSize

	if isTruncated {
		// Truncate with ellipsis
		return PayloadSample{
			Sample:      maskedData[:maxSize] + "...[truncated]",
			IsTruncated: true,
			FullSize:    fullSize,
		}
	}

	return PayloadSample{
		Sample:      maskedData,
		IsTruncated: false,
		FullSize:    fullSize,
	}
}

// maskPattern masks sensitive data in strings
func maskPattern(input string, pattern string) string {
	// Common masking patterns for K8s/cloud credentials
	patterns := map[string]*regexp.Regexp{
		"password":                   regexp.MustCompile(`(?i)"password":\s*"[^"]+"`),
		"secret":                     regexp.MustCompile(`(?i)"secret":\s*"[^"]+"`),
		"token":                      regexp.MustCompile(`(?i)"token":\s*"[^"]+"`),
		"api_key":                    regexp.MustCompile(`(?i)"api[_-]?key":\s*"[^"]+"`),
		"certificate-authority-data": regexp.MustCompile(`"certificate-authority-data":\s*"[A-Za-z0-9+/=]+"`),
		"client-certificate-data":    regexp.MustCompile(`"client-certificate-data":\s*"[A-Za-z0-9+/=]+"`),
		"client-key-data":            regexp.MustCompile(`"client-key-data":\s*"[A-Za-z0-9+/=]+"`),
	}

	if re, ok := patterns[pattern]; ok {
		return re.ReplaceAllString(input, fmt.Sprintf(`"%s": "[REDACTED]"`, pattern))
	}

	// Fallback to simple string replacement
	return strings.ReplaceAll(input, pattern, "[REDACTED]")
}

// shouldSample determines if sampling should occur based on rate
func shouldSample(rate float64) bool {
	if rate >= 1.0 {
		return true
	}
	if rate <= 0.0 {
		return false
	}
	// Simple random sampling (in production, use consistent hash)
	return (float64(os.Getpid()%100) / 100.0) < rate
}
