package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

type PulseHandler struct {
	logger *zap.Logger
	events []Event
	mu     sync.RWMutex
}

type Event struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Service   string    `json:"service"`
	Version   string    `json:"version"`
}

type StatusResponse struct {
	Service    string    `json:"service"`
	Status     string    `json:"status"`
	Uptime     string    `json:"uptime"`
	EventCount int       `json:"event_count"`
	Timestamp  time.Time `json:"timestamp"`
}

type HeartbeatRequest struct {
	Source  string                 `json:"source"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

type HeartbeatResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

var startTime = time.Now()

func NewPulseHandler(logger *zap.Logger) *PulseHandler {
	return &PulseHandler{
		logger: logger.Named("pulse-handler"),
		events: make([]Event, 0),
	}
}

// Health returns basic health status
func (h *PulseHandler) Health(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now(),
		Service:   "aziron-pulse",
		Version:   "v0.1.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Ready checks if service is ready to accept traffic
func (h *PulseHandler) Ready(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "ready",
		Timestamp: time.Now(),
		Service:   "aziron-pulse",
		Version:   "v0.1.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Live checks if service is alive
func (h *PulseHandler) Live(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "alive",
		Timestamp: time.Now(),
		Service:   "aziron-pulse",
		Version:   "v0.1.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetStatus returns detailed service status
func (h *PulseHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	eventCount := len(h.events)
	h.mu.RUnlock()

	uptime := time.Since(startTime)

	response := StatusResponse{
		Service:    "aziron-pulse",
		Status:     "running",
		Uptime:     uptime.String(),
		EventCount: eventCount,
		Timestamp:  time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	h.logger.Info("Status requested", zap.Int("event_count", eventCount))
}

// Heartbeat receives heartbeat signals from clients
func (h *PulseHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode heartbeat request", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.logger.Info("Heartbeat received",
		zap.String("source", req.Source),
		zap.String("message", req.Message),
	)

	// Store as event
	event := Event{
		ID:        generateID(),
		Type:      "heartbeat",
		Message:   req.Message,
		Timestamp: time.Now(),
	}

	h.mu.Lock()
	h.events = append(h.events, event)
	// Keep only last 1000 events
	if len(h.events) > 1000 {
		h.events = h.events[len(h.events)-1000:]
	}
	h.mu.Unlock()

	response := HeartbeatResponse{
		Status:    "received",
		Timestamp: time.Now(),
		Message:   "Heartbeat recorded successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetEvents returns recent events
func (h *PulseHandler) GetEvents(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	events := make([]Event, len(h.events))
	copy(events, h.events)
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)

	h.logger.Info("Events requested", zap.Int("count", len(events)))
}

// CreateEvent creates a new event
func (h *PulseHandler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	var event Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		h.logger.Error("Failed to decode event", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	event.ID = generateID()
	event.Timestamp = time.Now()

	h.mu.Lock()
	h.events = append(h.events, event)
	// Keep only last 1000 events
	if len(h.events) > 1000 {
		h.events = h.events[len(h.events)-1000:]
	}
	h.mu.Unlock()

	h.logger.Info("Event created",
		zap.String("id", event.ID),
		zap.String("type", event.Type),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(event)
}

// generateID generates a simple ID for events
func generateID() string {
	return time.Now().Format("20060102150405.000000")
}
