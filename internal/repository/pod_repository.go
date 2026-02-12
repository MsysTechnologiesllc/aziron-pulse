package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/db"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/models"
	"github.com/google/uuid"
)

// PodRepository handles pod-related database operations
type PodRepository struct {
	db *db.DB
}

// NewPodRepository creates a new pod repository
func NewPodRepository(database *db.DB) *PodRepository {
	return &PodRepository{
		db: database,
	}
}

// Create creates a new pod record
func (r *PodRepository) Create(ctx context.Context, pod *models.PulsePod) error {
	query := `
		INSERT INTO pulse.pods (
			pulse_id, user_id, tenant_id, pod_name, namespace, service_name, pvc_name,
			node_port, base_image, cpu_limit, memory_limit_mb, storage_gb,
			workspace_path, status, ttl_minutes, expires_at, last_activity_at, metadata
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
		RETURNING id, created_at, updated_at
	`

	rows, err := r.db.QueryContext(ctx, query,
		pod.PulseID, pod.UserID, pod.TenantID, pod.PodName, pod.Namespace, pod.ServiceName, pod.PVCName,
		pod.NodePort, pod.BaseImage, pod.CPULimit, pod.MemoryLimitMB, pod.StorageGB,
		pod.WorkspacePath, pod.Status, pod.TTLMinutes, pod.ExpiresAt, pod.LastActivityAt, pod.Metadata,
	)
	if err != nil {
		return fmt.Errorf("failed to create pod: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&pod.ID, &pod.CreatedAt, &pod.UpdatedAt); err != nil {
			return fmt.Errorf("failed to scan created pod: %w", err)
		}
	}

	return nil
}

// GetByID retrieves a pod by ID
func (r *PodRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.PulsePod, error) {
	query := `
		SELECT * FROM pulse.pods
		WHERE id = $1 AND deleted_at IS NULL
	`

	var pod models.PulsePod
	if err := r.db.GetContext(ctx, &pod, query, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("pod not found")
		}
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	return &pod, nil
}

// GetByPulseID retrieves a pod by pulse_id
func (r *PodRepository) GetByPulseID(ctx context.Context, pulseID string) (*models.PulsePod, error) {
	query := `
		SELECT * FROM pulse.pods
		WHERE pulse_id = $1 AND deleted_at IS NULL
	`

	var pod models.PulsePod
	if err := r.db.GetContext(ctx, &pod, query, pulseID); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("pod not found")
		}
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	return &pod, nil
}

// ListByUserID retrieves all pods for a user
func (r *PodRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*models.PulsePod, error) {
	query := `
		SELECT * FROM pulse.pods
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	var pods []*models.PulsePod
	if err := r.db.SelectContext(ctx, &pods, query, userID); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return pods, nil
}

// CountActiveByUserID counts active (non-deleted) pods for a user
func (r *PodRepository) CountActiveByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*) FROM pulse.pods
		WHERE user_id = $1 AND deleted_at IS NULL
		AND status NOT IN ('terminated', 'failed', 'expired')
	`

	var count int
	if err := r.db.GetContext(ctx, &count, query, userID); err != nil {
		return 0, fmt.Errorf("failed to count pods: %w", err)
	}

	return count, nil
}

// UpdateStatus updates the pod status
func (r *PodRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `
		UPDATE pulse.pods
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`

	result, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("pod not found")
	}

	return nil
}

// UpdateActivity updates the last_activity_at and recalculates expires_at
func (r *PodRepository) UpdateActivity(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE pulse.pods
		SET 
			last_activity_at = NOW(),
			expires_at = NOW() + (ttl_minutes || ' minutes')::INTERVAL,
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to update activity: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("pod not found")
	}

	return nil
}

// SoftDelete marks a pod as deleted
func (r *PodRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE pulse.pods
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to soft delete pod: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("pod not found")
	}

	return nil
}

// GetExpiredPods retrieves all pods that have expired
func (r *PodRepository) GetExpiredPods(ctx context.Context) ([]*models.PulsePod, error) {
	query := `
		SELECT * FROM pulse.pods
		WHERE expires_at IS NOT NULL
		AND expires_at < NOW()
		AND deleted_at IS NULL
		AND status NOT IN ('terminated', 'expired')
		ORDER BY expires_at ASC
	`

	var pods []*models.PulsePod
	if err := r.db.SelectContext(ctx, &pods, query); err != nil {
		return nil, fmt.Errorf("failed to get expired pods: %w", err)
	}

	return pods, nil
}
