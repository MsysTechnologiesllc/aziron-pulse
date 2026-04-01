package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/k8s"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/middleware"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/service"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// ProxyHandler handles proxying requests to code-server pods
type ProxyHandler struct {
	provisionSvc   *service.ProvisionService
	portForwardMgr *k8s.PortForwardManager // non-nil when K8S_NODE_IP is set (local dev)
	logger         *zap.Logger
}

// NewProxyHandler creates a new proxy handler.
// portForwardMgr may be nil (production); when set it is used for local dev
// environments where the cluster network is not directly reachable from the host.
func NewProxyHandler(provisionSvc *service.ProvisionService, portForwardMgr *k8s.PortForwardManager, logger *zap.Logger) *ProxyHandler {
	return &ProxyHandler{
		provisionSvc:   provisionSvc,
		portForwardMgr: portForwardMgr,
		logger:         logger,
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

	// Build target URL.
	// When K8S_NODE_IP is set (local dev / minikube) we use a kubectl-style
	// port-forward through the Kubernetes API server so that the pod is
	// reachable from the host even when the cluster network (svc.cluster.local,
	// NodePort) is not directly accessible (e.g. Docker-driver minikube on macOS).
	// Otherwise use cluster-internal DNS (in-cluster production deployment).
	var target string
	if os.Getenv("K8S_NODE_IP") != "" && h.portForwardMgr != nil {
		localPort, pfErr := h.portForwardMgr.LocalPort(pod.Namespace, pod.PodName, 8080)
		if pfErr != nil {
			h.logger.Error("Port-forward failed", zap.String("pulse_id", pulseID), zap.Error(pfErr))
			http.Error(w, "Service unavailable", http.StatusBadGateway)
			return
		}
		target = fmt.Sprintf("http://127.0.0.1:%d", localPort)
	} else {
		target = fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", pod.ServiceName, pod.Namespace)
	}

	targetURL, err := url.Parse(target)
	if err != nil {
		h.logger.Error("Failed to parse target URL", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Proxying request to pod",
		zap.String("pulse_id", pulseID),
		zap.String("method", r.Method),
		zap.String("incoming_path", r.URL.Path),
		zap.String("target", target),
		zap.String("upgrade", r.Header.Get("Upgrade")),
	)

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Customize director to rewrite paths
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
		req.URL.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme

		// Strip the Origin header so code-server's built-in origin check doesn't
		// reject the request. The browser sends the Aziron UI origin which
		// differs from the pod's cluster-internal host, causing "Origin not allowed".
		req.Header.Del("Origin")

		// Rewrite path: /pulse/{pulse_id}/foo -> /foo
		prefix := fmt.Sprintf("/pulse/%s", pulseID)
		rewritten := strings.TrimPrefix(r.URL.Path, prefix)
		if rewritten == "" {
			rewritten = "/"
		}
		h.logger.Debug("Proxy path rewrite",
			zap.String("pulse_id", pulseID),
			zap.String("original", r.URL.Path),
			zap.String("rewritten", rewritten),
		)
		req.URL.Path = rewritten
	}

	// Error handler
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		h.logger.Error("Proxy error",
			zap.Error(err),
			zap.String("pulse_id", pulseID),
			zap.String("path", r.URL.Path),
			zap.String("target", target),
		)
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
