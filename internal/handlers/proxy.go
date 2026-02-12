package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/middleware"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/service"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// ProxyHandler handles proxying requests to code-server pods
type ProxyHandler struct {
	provisionSvc *service.ProvisionService
	logger       *zap.Logger
}

// NewProxyHandler creates a new proxy handler
func NewProxyHandler(provisionSvc *service.ProvisionService, logger *zap.Logger) *ProxyHandler {
	return &ProxyHandler{
		provisionSvc: provisionSvc,
		logger:       logger,
	}
}

// ProxyToPod handles requests to /pulse/{pulse_id}/*
func (h *ProxyHandler) ProxyToPod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	pulseID := vars["pulse_id"]

	// Get user ID from context
	userID, err := middleware.GetUserID(ctx)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get pod
	pod, err := h.provisionSvc.GetPod(ctx, pulseID)
	if err != nil {
		http.Error(w, "Pod not found", http.StatusNotFound)
		return
	}

	// Verify ownership
	if pod.UserID != userID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Update activity timestamp
	_ = h.provisionSvc.UpdatePodActivity(ctx, pulseID)

	// Build target URL
	var target string
	if pod.NodePort != nil {
		// Use NodePort for external access
		target = fmt.Sprintf("http://localhost:%d", *pod.NodePort)
	} else {
		// Use cluster-internal service
		target = fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", pod.ServiceName, pod.Namespace)
	}

	targetURL, err := url.Parse(target)
	if err != nil {
		h.logger.Error("Failed to parse target URL", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Customize director to rewrite paths
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
		req.URL.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme

		// Rewrite path: /pulse/{pulse_id}/foo -> /foo
		prefix := fmt.Sprintf("/pulse/%s", pulseID)
		req.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
	}

	// Error handler
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		h.logger.Error("Proxy error", zap.Error(err), zap.String("pulse_id", pulseID))
		http.Error(w, "Service unavailable", http.StatusBadGateway)
	}

	proxy.ServeHTTP(w, r)
}

// HealthCheck handles GET /pulse/{pulse_id}/health
func (h *ProxyHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	pulseID := vars["pulse_id"]

	// Get user ID from context
	userID, err := middleware.GetUserID(ctx)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get pod
	pod, err := h.provisionSvc.GetPod(ctx, pulseID)
	if err != nil {
		http.Error(w, "Pod not found", http.StatusNotFound)
		return
	}

	// Verify ownership
	if pod.UserID != userID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pulse_id":  pod.PulseID,
		"status":    pod.Status,
		"namespace": pod.Namespace,
		"pod_name":  pod.PodName,
		"node_port": pod.NodePort,
	})
}
