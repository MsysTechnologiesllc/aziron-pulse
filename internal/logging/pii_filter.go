package logging

import (
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap/zapcore"
)

// FilterMode determines the filtering strategy
type FilterMode string

const (
	FilterModeStrict  FilterMode = "strict"  // Apply all filters regardless of log level
	FilterModeRelaxed FilterMode = "relaxed" // Relaxed filtering for DEBUG in dev
	FilterModeOff     FilterMode = "off"     // Disable filtering
)

// FilterConfig holds configuration for sensitive data filtering
type FilterConfig struct {
	Mode        FilterMode
	Environment string
}

// SensitiveDataFilter provides methods to redact sensitive information from logs
type SensitiveDataFilter struct {
	config FilterConfig

	// Compiled regex patterns
	emailPattern          *regexp.Regexp
	phonePattern          *regexp.Regexp
	ssnPattern            *regexp.Regexp
	creditCardPattern     *regexp.Regexp
	jwtPattern            *regexp.Regexp
	bearerPattern         *regexp.Regexp
	apiKeyPatterns        []*regexp.Regexp
	dbURLPattern          *regexp.Regexp
	kubeconfigCertPattern *regexp.Regexp
	kubeconfigClientCert  *regexp.Regexp
	kubeconfigClientKey   *regexp.Regexp
	kubeconfigPathPattern *regexp.Regexp

	// Field-based denylist
	sensitiveFields map[string]bool
}

// NewSensitiveDataFilter creates a new filter with compiled regex patterns
func NewSensitiveDataFilter(config FilterConfig) *SensitiveDataFilter {
	filter := &SensitiveDataFilter{
		config: config,
		sensitiveFields: map[string]bool{
			"password":                   true,
			"password_hash":              true,
			"token":                      true,
			"access_token":               true,
			"refresh_token":              true,
			"api_key":                    true,
			"apikey":                     true,
			"secret":                     true,
			"secret_key":                 true,
			"encryption_key":             true,
			"jwt":                        true,
			"jwt_secret":                 true,
			"client_secret":              true,
			"oauth_token":                true,
			"provider_token":             true,
			"database_url":               true,
			"connection_string":          true,
			"certificate-authority-data": true,
			"client-certificate-data":    true,
			"client-key-data":            true,
			"kubeconfig":                 true,
		},
	}

	// Compile regex patterns
	filter.emailPattern = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`)
	filter.phonePattern = regexp.MustCompile(`\+\d{1,3}[-.\s]?\(?\d{1,4}\)?[-.\s]?\d{1,4}[-.\s]?\d{1,9}|\(\d{3}\)\s?\d{3}-\d{4}`)
	filter.ssnPattern = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	filter.creditCardPattern = regexp.MustCompile(`\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`)
	filter.jwtPattern = regexp.MustCompile(`\beyJ[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+\.[A-Za-z0-9-_]*\b`)
	filter.bearerPattern = regexp.MustCompile(`Bearer\s+[A-Za-z0-9\-._~+/]+=*`)
	filter.dbURLPattern = regexp.MustCompile(`(postgres|mysql|mongodb|redis)://([^:]+):([^@]+)@([^/]+)/(\w+)`)

	// Kubeconfig-specific patterns for certificate data
	filter.kubeconfigCertPattern = regexp.MustCompile(`certificate-authority-data:\s*[A-Za-z0-9+/=]+`)
	filter.kubeconfigClientCert = regexp.MustCompile(`client-certificate-data:\s*[A-Za-z0-9+/=]+`)
	filter.kubeconfigClientKey = regexp.MustCompile(`client-key-data:\s*[A-Za-z0-9+/=]+`)
	filter.kubeconfigPathPattern = regexp.MustCompile(`/Users/[^/]+/.kube/config`)

	// API key patterns
	filter.apiKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{32,}\b`),
		regexp.MustCompile(`\bapi_key_[A-Za-z0-9_-]+\b`),
		regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{35}\b`),
	}

	return filter
}

// shouldFilter determines if filtering should be applied
func (f *SensitiveDataFilter) shouldFilter(level zapcore.Level) bool {
	if f.config.Mode == FilterModeOff {
		return false
	}

	if f.config.Mode == FilterModeStrict {
		return true
	}

	// FilterModeRelaxed
	if f.config.Environment == "production" {
		return true
	}

	// In development with relaxed mode, skip DEBUG filtering
	if level == zapcore.DebugLevel {
		return false
	}

	return level >= zapcore.InfoLevel
}

// FilterField redacts a field value if the field name is sensitive
func (f *SensitiveDataFilter) FilterField(key string, value interface{}) interface{} {
	keyLower := strings.ToLower(key)
	if f.sensitiveFields[keyLower] {
		return f.redactByFieldName(keyLower, value)
	}

	if str, ok := value.(string); ok {
		return f.FilterString(str)
	}

	return value
}

// FilterString applies regex-based filtering to a string value
func (f *SensitiveDataFilter) FilterString(value string) string {
	if value == "" {
		return value
	}

	filtered := value

	// Kubeconfig certificate data - MUST be filtered first
	if f.kubeconfigCertPattern.MatchString(filtered) {
		filtered = f.kubeconfigCertPattern.ReplaceAllString(filtered, "certificate-authority-data: [REDACTED-CERT]")
	}
	if f.kubeconfigClientCert.MatchString(filtered) {
		filtered = f.kubeconfigClientCert.ReplaceAllString(filtered, "client-certificate-data: [REDACTED-CERT]")
	}
	if f.kubeconfigClientKey.MatchString(filtered) {
		filtered = f.kubeconfigClientKey.ReplaceAllString(filtered, "client-key-data: [REDACTED-KEY]")
	}
	if f.kubeconfigPathPattern.MatchString(filtered) {
		filtered = f.kubeconfigPathPattern.ReplaceAllString(filtered, "[KUBECONFIG-PATH]")
	}

	// JWT tokens
	if f.jwtPattern.MatchString(filtered) {
		filtered = f.jwtPattern.ReplaceAllStringFunc(filtered, func(match string) string {
			if len(match) > 10 {
				return match[:10] + "...[REDACTED]"
			}
			return "[REDACTED]"
		})
	}

	// Bearer tokens
	if f.bearerPattern.MatchString(filtered) {
		filtered = f.bearerPattern.ReplaceAllString(filtered, "Bearer [REDACTED]")
	}

	// API keys
	for _, pattern := range f.apiKeyPatterns {
		if pattern.MatchString(filtered) {
			filtered = pattern.ReplaceAllStringFunc(filtered, func(match string) string {
				if strings.HasPrefix(match, "sk-") && len(match) > 6 {
					return "sk-****" + match[len(match)-3:]
				}
				if len(match) > 6 {
					return match[:3] + "****" + match[len(match)-3:]
				}
				return "****"
			})
		}
	}

	// Database URLs
	if f.dbURLPattern.MatchString(filtered) {
		filtered = f.dbURLPattern.ReplaceAllString(filtered, "$1://$2:[REDACTED]@$4/$5")
	}

	// Email addresses
	if f.emailPattern.MatchString(filtered) {
		filtered = f.emailPattern.ReplaceAllString(filtered, "[EMAIL]")
	}

	// SSN
	if f.ssnPattern.MatchString(filtered) {
		filtered = f.ssnPattern.ReplaceAllStringFunc(filtered, func(match string) string {
			parts := strings.Split(match, "-")
			if len(parts) == 3 {
				return "***-**-" + parts[2]
			}
			return "[SSN]"
		})
	}

	// Credit card
	if f.creditCardPattern.MatchString(filtered) {
		filtered = f.creditCardPattern.ReplaceAllStringFunc(filtered, func(match string) string {
			cleaned := strings.ReplaceAll(strings.ReplaceAll(match, "-", ""), " ", "")
			if len(cleaned) >= 4 {
				return "****-****-****-" + cleaned[len(cleaned)-4:]
			}
			return "****-****-****-****"
		})
	}

	// Phone numbers
	if f.phonePattern.MatchString(filtered) {
		filtered = f.phonePattern.ReplaceAllStringFunc(filtered, func(match string) string {
			if strings.Contains(match, "[") || strings.Contains(match, "*") {
				return match
			}
			if len(match) > 4 {
				return "[PHONE-****" + match[len(match)-4:] + "]"
			}
			return "[PHONE]"
		})
	}

	return filtered
}

// redactByFieldName returns type-specific redaction format
func (f *SensitiveDataFilter) redactByFieldName(key string, value interface{}) interface{} {
	if strings.Contains(key, "password") {
		return "****"
	}

	if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "key") {
		return "****"
	}

	if strings.Contains(key, "certificate") || strings.Contains(key, "cert") {
		return "[REDACTED-CERT]"
	}

	if strings.Contains(key, "kubeconfig") {
		return "[KUBECONFIG-PATH]"
	}

	if strings.Contains(key, "authorization") || strings.Contains(key, "bearer") {
		return "Bearer [REDACTED]"
	}

	if strings.Contains(key, "jwt") {
		if str, ok := value.(string); ok && len(str) > 10 {
			return str[:10] + "...[REDACTED]"
		}
		return "[REDACTED]"
	}

	if strings.Contains(key, "api") {
		if str, ok := value.(string); ok {
			if strings.HasPrefix(str, "sk-") && len(str) > 6 {
				return "sk-****" + str[len(str)-3:]
			}
			if len(str) > 6 {
				return str[:3] + "****" + str[len(str)-3:]
			}
		}
		return "****"
	}

	return "****"
}

// filterField applies full filtering
func (f *SensitiveDataFilter) filterField(field zapcore.Field) zapcore.Field {
	keyLower := strings.ToLower(field.Key)
	if f.sensitiveFields[keyLower] {
		return zapcore.Field{
			Key:    field.Key,
			Type:   zapcore.StringType,
			String: fmt.Sprintf("%v", f.redactByFieldName(keyLower, field.String)),
		}
	}

	if field.Type == zapcore.StringType {
		filtered := f.FilterString(field.String)
		if filtered != field.String {
			return zapcore.Field{
				Key:    field.Key,
				Type:   zapcore.StringType,
				String: filtered,
			}
		}
	}

	if field.Interface != nil {
		if str, ok := field.Interface.(string); ok {
			filtered := f.FilterString(str)
			if filtered != str {
				return zapcore.Field{
					Key:       field.Key,
					Type:      zapcore.StringType,
					String:    filtered,
					Interface: filtered,
				}
			}
		}
	}

	return field
}

// FilterFields filters all fields in a field slice
func (f *SensitiveDataFilter) FilterFields(fields []zapcore.Field, level zapcore.Level) []zapcore.Field {
	if !f.shouldFilter(level) && f.config.Environment != "production" {
		// Field-based only
		filtered := make([]zapcore.Field, len(fields))
		for i, field := range fields {
			keyLower := strings.ToLower(field.Key)
			if f.sensitiveFields[keyLower] {
				filtered[i] = zapcore.Field{
					Key:    field.Key,
					Type:   zapcore.StringType,
					String: fmt.Sprintf("%v", f.redactByFieldName(keyLower, field.String)),
				}
			} else {
				filtered[i] = field
			}
		}
		return filtered
	}

	// Full filtering
	filtered := make([]zapcore.Field, len(fields))
	for i, field := range fields {
		filtered[i] = f.filterField(field)
	}
	return filtered
}

// FilterMessage applies filtering to log messages
func (f *SensitiveDataFilter) FilterMessage(message string, level zapcore.Level) string {
	if !f.shouldFilter(level) {
		return message
	}
	return f.FilterString(message)
}
