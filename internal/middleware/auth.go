package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type contextKey string

const (
	userIDKey    contextKey = "user_id"
	tenantIDKey  contextKey = "tenant_id"
	userEmailKey contextKey = "user_email"
	jwtTokenKey  contextKey = "jwt_token" // Raw JWT token for code-server authentication
)

// AuthMiddleware validates JWT tokens and extracts user information
type AuthMiddleware struct {
	jwtSecret []byte
}

// NewAuthMiddleware creates a new auth middleware
func NewAuthMiddleware(jwtSecret string) *AuthMiddleware {
	return &AuthMiddleware{
		jwtSecret: []byte(jwtSecret),
	}
}

// Authenticate validates the JWT token and adds user context
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tokenString string
		
		// Try to get token from Authorization header first
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			// Extract token from "Bearer <token>"
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenString = parts[1]
			}
		}
		
		// If no header token, check query parameter (for iframe support)
		if tokenString == "" {
			tokenString = r.URL.Query().Get("token")
		}
		
		// If still no token, return unauthorized
		if tokenString == "" {
			http.Error(w, "Missing authorization header", http.StatusUnauthorized)
			return
		}

		// Parse and validate token
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return m.jwtSecret, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Extract claims
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Invalid token claims", http.StatusUnauthorized)
			return
		}

		// Extract user_id
		userIDStr, ok := claims["user_id"].(string)
		if !ok {
			http.Error(w, "Missing user_id in token", http.StatusUnauthorized)
			return
		}

		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			http.Error(w, "Invalid user_id format", http.StatusUnauthorized)
			return
		}

		// Extract tenant_id (optional)
		var tenantID *uuid.UUID
		if tenantIDStr, ok := claims["tenant_id"].(string); ok && tenantIDStr != "" {
			tid, err := uuid.Parse(tenantIDStr)
			if err == nil {
				tenantID = &tid
			}
		}

		// Extract email (optional but recommended for telemetry)
		userEmail := ""
		if email, ok := claims["email"].(string); ok {
			userEmail = email
		}

		// Add to context
		ctx := context.WithValue(r.Context(), userIDKey, userID)
		ctx = context.WithValue(ctx, jwtTokenKey, tokenString) // Store raw token
		if tenantID != nil {
			ctx = context.WithValue(ctx, tenantIDKey, *tenantID)
		}
		if userEmail != "" {
			ctx = context.WithValue(ctx, userEmailKey, userEmail)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUserID retrieves the user ID from context
func GetUserID(ctx context.Context) (uuid.UUID, error) {
	userID, ok := ctx.Value(userIDKey).(uuid.UUID)
	if !ok {
		return uuid.UUID{}, fmt.Errorf("user_id not found in context")
	}
	return userID, nil
}

// GetUserEmail retrieves the user email from context
func GetUserEmail(ctx context.Context) string {
	userEmail, _ := ctx.Value(userEmailKey).(string)
	return userEmail
}

// GetTenantID retrieves the tenant ID from context
func GetTenantID(ctx context.Context) (*uuid.UUID, error) {
	tenantID, ok := ctx.Value(tenantIDKey).(uuid.UUID)
	if !ok {
		return nil, nil // Tenant ID is optional
	}
	return &tenantID, nil
}

// GetJWTToken retrieves the raw JWT token from context
func GetJWTToken(ctx context.Context) (string, error) {
	token, ok := ctx.Value(jwtTokenKey).(string)
	if !ok {
		return "", fmt.Errorf("jwt_token not found in context")
	}
	return token, nil
}
