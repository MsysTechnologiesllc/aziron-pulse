package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/db"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/models"
	"github.com/google/uuid"
)

// QuotaRepository handles user quota database operations
type QuotaRepository struct {
	db *db.DB
}

// NewQuotaRepository creates a new quota repository
func NewQuotaRepository(database *db.DB) *QuotaRepository {
	return &QuotaRepository{
		db: database,
	}
}

// GetByUserID retrieves a user's quota
func (r *QuotaRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*models.UserQuota, error) {
	query := `
		SELECT * FROM pulse.quotas
		WHERE user_id = $1
	`

	var quota models.UserQuota
	if err := r.db.GetContext(ctx, &quota, query, userID); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("quota not found")
		}
		return nil, fmt.Errorf("failed to get quota: %w", err)
	}

	return &quota, nil
}

// GetOrCreateDefault retrieves a user's quota or creates default if not exists
func (r *QuotaRepository) GetOrCreateDefault(ctx context.Context, userID uuid.UUID) (*models.UserQuota, error) {
	// Try to get existing quota
	quota, err := r.GetByUserID(ctx, userID)
	if err == nil {
		return quota, nil
	}

	// Create default quota
	defaultQuota := &models.UserQuota{
		UserID:             userID,
		MaxPods:            3,
		MaxCPUPerPod:       2.0,
		MaxMemoryMBPerPod:  4096,
		MaxStorageGBPerPod: 10,
	}

	if err := r.Create(ctx, defaultQuota); err != nil {
		// Handle race condition - quota was created between get and create
		quota, getErr := r.GetByUserID(ctx, userID)
		if getErr == nil {
			return quota, nil
		}
		return nil, err
	}

	return defaultQuota, nil
}

// Create creates a new quota
func (r *QuotaRepository) Create(ctx context.Context, quota *models.UserQuota) error {
	query := `
		INSERT INTO pulse.quotas (
			user_id, max_pods, max_cpu_per_pod, max_memory_mb_per_pod, max_storage_gb_per_pod
		) VALUES (
			$1, $2, $3, $4, $5
		)
		RETURNING id, created_at, updated_at
	`

	rows, err := r.db.QueryContext(ctx, query,
		quota.UserID, quota.MaxPods, quota.MaxCPUPerPod, quota.MaxMemoryMBPerPod, quota.MaxStorageGBPerPod,
	)
	if err != nil {
		return fmt.Errorf("failed to create quota: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&quota.ID, &quota.CreatedAt, &quota.UpdatedAt); err != nil {
			return fmt.Errorf("failed to scan created quota: %w", err)
		}
	}

	return nil
}

// Update updates a user's quota
func (r *QuotaRepository) Update(ctx context.Context, quota *models.UserQuota) error {
	query := `
		UPDATE pulse.quotas
		SET 
			max_pods = $1,
			max_cpu_per_pod = $2,
			max_memory_mb_per_pod = $3,
			max_storage_gb_per_pod = $4,
			updated_at = NOW()
		WHERE user_id = $5
	`

	result, err := r.db.ExecContext(ctx, query,
		quota.MaxPods, quota.MaxCPUPerPod, quota.MaxMemoryMBPerPod, quota.MaxStorageGBPerPod, quota.UserID,
	)
	if err != nil {
		return fmt.Errorf("failed to update quota: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("quota not found")
	}

	return nil
}
