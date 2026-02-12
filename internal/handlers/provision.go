package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/middleware"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/models"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/service"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// ProvisionHandler handles pod provisioning requests
type ProvisionHandler struct {
	provisionSvc *service.ProvisionService
	logger       *zap.Logger
}

// NewProvisionHandler creates a new provision handler
func NewProvisionHandler(provisionSvc *service.ProvisionService, logger *zap.Logger) *ProvisionHandler {
	return &ProvisionHandler{
		provisionSvc: provisionSvc,
		logger:       logger,
	}
}

// ProvisionRequest represents the provision API request
type ProvisionRequest struct {
	BaseImage string                 `json:"base_image,omitempty"`
	CPULimit  *float64               `json:"cpu_limit,omitempty"`
	MemoryMB  *int                   `json:"memory_mb,omitempty"`
	StorageGB *int                   `json:"storage_gb,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ProvisionPod handles POST /provision
func (h *ProvisionHandler) ProvisionPod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userID, err := middleware.GetUserID(ctx)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get JWT token from context for code-server authentication
	jwtToken, err := middleware.GetJWTToken(ctx)
	if err != nil {
		h.logger.Warn("JWT token not found in context, using default password")
		jwtToken = "aziron" // Fallback to default
	}

	tenantID, _ := middleware.GetTenantID(ctx)

	// Parse request
	var req ProvisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.BaseImage == "" {
		req.BaseImage = "codercom/code-server:latest"
	}
	if req.CPULimit == nil {
		cpu := 2.0
		req.CPULimit = &cpu
	}
	if req.MemoryMB == nil {
		mem := 4096
		req.MemoryMB = &mem
	}
	if req.StorageGB == nil {
		storage := 10
		req.StorageGB = &storage
	}

	// Provision pod
	pod, err := h.provisionSvc.ProvisionPod(ctx, service.ProvisionRequest{
		UserID:    userID,
		TenantID:  tenantID,
		BaseImage: req.BaseImage,
		CPULimit:  *req.CPULimit,
		MemoryMB:  *req.MemoryMB,
		StorageGB: *req.StorageGB,
		Metadata:  models.JSONBMap(req.Metadata),
		JWTToken:  jwtToken, // Pass JWT token for code-server password
	})
	if err != nil {
		h.logger.Error("Failed to provision pod", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pod)
}

// GetPod handles GET /provision/{pulse_id}
func (h *ProvisionHandler) GetPod(w http.ResponseWriter, r *http.Request) {
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
	json.NewEncoder(w).Encode(pod)
}

// ListPods handles GET /provision
func (h *ProvisionHandler) ListPods(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userID, err := middleware.GetUserID(ctx)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// List pods
	pods, err := h.provisionSvc.ListUserPods(ctx, userID)
	if err != nil {
		h.logger.Error("Failed to list pods", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pods":  pods,
		"count": len(pods),
	})
}

// DeletePod handles DELETE /provision/{pulse_id}
func (h *ProvisionHandler) DeletePod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	pulseID := vars["pulse_id"]

	// Get user ID from context
	userID, err := middleware.GetUserID(ctx)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get pod to verify ownership
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

	// Delete pod
	if err := h.provisionSvc.DeletePod(ctx, pulseID); err != nil {
		h.logger.Error("Failed to delete pod", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
