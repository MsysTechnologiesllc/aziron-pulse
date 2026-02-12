package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

// PulseHandler handles basic health and status endpoints
type PulseHandler struct {
	startTime time.Time
}

// NewPulseHandler creates a new pulse handler
func NewPulseHandler() *PulseHandler {
	return &PulseHandler{
		startTime: time.Now(),
	}
}

// Health handles GET /health
func (h *PulseHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"uptime": time.Since(h.startTime).String(),
	})
}

// Status handles GET /status
func (h *PulseHandler) Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"service":    "aziron-pulse",
		"version":    "1.0.0",
		"started_at": h.startTime.Format(time.RFC3339),
		"uptime":     time.Since(h.startTime).String(),
	})
}

// Heartbeat handles POST /heartbeat
func (h *PulseHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"message":   "heartbeat received",
	})
}
