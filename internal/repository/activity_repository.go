package repository

import (
	"context"
	"fmt"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/db"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/models"
	"github.com/google/uuid"
)

// ActivityRepository handles pod activity logging
type ActivityRepository struct {
	db *db.DB
}

// NewActivityRepository creates a new activity repository
func NewActivityRepository(database *db.DB) *ActivityRepository {
	return &ActivityRepository{
		db: database,
	}
}

// Create creates a new activity log entry
func (r *ActivityRepository) Create(ctx context.Context, activity *models.PodActivity) error {
	query := `
		INSERT INTO pulse.activities (
			pulse_id, activity_type, description, metadata
		) VALUES (
			$1, $2, $3, $4
		)
		RETURNING id, created_at
	`

	rows, err := r.db.QueryContext(ctx, query,
		activity.PodID, activity.ActivityType, activity.Description, activity.Metadata,
	)
	if err != nil {
		return fmt.Errorf("failed to create activity: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&activity.ID, &activity.CreatedAt); err != nil {
			return fmt.Errorf("failed to scan created activity: %w", err)
		}
	}

	return nil
}

// ListByPulseID retrieves all activities for a pulse ID (pod ID)
func (r *ActivityRepository) ListByPulseID(ctx context.Context, podID uuid.UUID, limit int) ([]*models.PodActivity, error) {
	query := `
		SELECT * FROM pulse.activities
		WHERE pulse_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	var activities []*models.PodActivity
	if err := r.db.SelectContext(ctx, &activities, query, podID, limit); err != nil {
		return nil, fmt.Errorf("failed to list activities: %w", err)
	}

	return activities, nil
}

// DeleteByPulseID deletes all activities for a pulse ID (pod ID)
func (r *ActivityRepository) DeleteByPulseID(ctx context.Context, podID uuid.UUID) error {
	query := `
		DELETE FROM pulse.activities
		WHERE pulse_id = $1
	`

	if _, err := r.db.ExecContext(ctx, query, podID); err != nil {
		return fmt.Errorf("failed to delete activities: %w", err)
	}

	return nil
}
