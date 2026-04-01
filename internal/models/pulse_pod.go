package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PulsePod represents a provisioned Kubernetes pod for development
type PulsePod struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	PulseID        string     `json:"pulse_id" db:"pulse_id"`
	UserID         uuid.UUID  `json:"user_id" db:"user_id"`
	TenantID       *uuid.UUID `json:"tenant_id,omitempty" db:"tenant_id"`
	Namespace      string     `json:"namespace" db:"namespace"`
	PodName        string     `json:"pod_name" db:"pod_name"`
	ServiceName    string     `json:"service_name" db:"service_name"`
	PVCName        string     `json:"pvc_name" db:"pvc_name"`
	NodePort       *int       `json:"node_port,omitempty" db:"node_port"`
	Status         string     `json:"status" db:"status"`
	BaseImage      string     `json:"base_image" db:"base_image"`
	CPULimit       float64    `json:"cpu_limit" db:"cpu_limit"`
	MemoryLimitMB  int        `json:"memory_limit_mb" db:"memory_limit_mb"`
	StorageGB      int        `json:"storage_gb" db:"storage_gb"`
	WorkspacePath  string     `json:"workspace_path" db:"workspace_path"`
	LastActivityAt time.Time  `json:"last_activity_at" db:"last_activity_at"`
	TTLMinutes     int        `json:"ttl_minutes" db:"ttl_minutes"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	TraceID        *string    `json:"trace_id,omitempty" db:"trace_id"`
	SpanID         *string    `json:"span_id,omitempty" db:"span_id"`
	UserEmail      *string    `json:"user_email,omitempty" db:"user_email"`
	RepoURL        *string    `json:"repo_url,omitempty" db:"repo_url"`
	ResourceTier   string     `json:"resource_tier,omitempty" db:"resource_tier"`
	Metadata       JSONBMap   `json:"metadata,omitempty" db:"metadata"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
}

// PodActivity tracks activities on a pod
type PodActivity struct {
	ID           uuid.UUID `json:"id" db:"id"`
	PodID        uuid.UUID `json:"pod_id" db:"pulse_id"` // FK to pulse.pods.id (column name is pulse_id in schema)
	ActivityType string    `json:"activity_type" db:"activity_type"`
	Description  string    `json:"description,omitempty" db:"description"`
	Metadata     JSONBMap  `json:"metadata,omitempty" db:"metadata"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// UserQuota represents resource quotas for a user
type UserQuota struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	UserID             uuid.UUID  `json:"user_id" db:"user_id"`
	TenantID           *uuid.UUID `json:"tenant_id,omitempty" db:"tenant_id"`
	MaxPods            int        `json:"max_pods" db:"max_pods"`
	MaxCPUPerPod       float64    `json:"max_cpu_per_pod" db:"max_cpu_per_pod"`
	MaxMemoryMBPerPod  int        `json:"max_memory_mb_per_pod" db:"max_memory_mb_per_pod"`
	MaxStorageGBPerPod int        `json:"max_storage_gb_per_pod" db:"max_storage_gb_per_pod"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

// JSONBMap is a custom type for JSONB fields
type JSONBMap map[string]interface{}

// Value implements the driver.Valuer interface
func (j JSONBMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements the sql.Scanner interface
func (j *JSONBMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}

	return json.Unmarshal(bytes, j)
}

// Pod status constants
const (
	PodStatusPending    = "pending"
	PodStatusRunning    = "running"
	PodStatusTerminated = "terminated"
	PodStatusFailed     = "failed"
	PodStatusExpired    = "expired"
)

// Activity types
const (
	ActivityTypeCreated  = "created"
	ActivityTypeStarted  = "started"
	ActivityTypeAccessed = "accessed"
	ActivityTypeStopped  = "stopped"
	ActivityTypeDeleted  = "deleted"
	ActivityTypeExpired  = "expired"
	ActivityTypeError    = "error"
)
