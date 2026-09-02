package repositories

import (
	"errors"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// ErrNotConfigured is returned by Get when no settings row exists yet (fresh
// deployment). It is deliberately distinct from a database error so callers
// (buildSolverRequest in ticket 136.c) can fall back to defaults on
// ErrNotConfigured while still failing loudly on a real DB failure.
var ErrNotConfigured = errors.New("generation settings not configured")

type GenerationSettingsRepository interface {
	// Get returns the singleton settings row. When no row exists yet it
	// returns populated defaults AND ErrNotConfigured (not a hard failure).
	Get() (*models.GenerationSettings, error)
	// Upsert creates or updates the singleton row (fixed primary key).
	Upsert(settings *models.GenerationSettings) error
}

type generationSettingsRepository struct {
	db *gorm.DB
}

func NewGenerationSettingsRepository(db *gorm.DB) GenerationSettingsRepository {
	return &generationSettingsRepository{db: db}
}

func (r *generationSettingsRepository) Get() (*models.GenerationSettings, error) {
	var settings models.GenerationSettings
	err := r.db.First(&settings, models.SingletonID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Populated defaults + sentinel: callers choose to fall back or fail.
		return models.DefaultGenerationSettings(), ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (r *generationSettingsRepository) Upsert(settings *models.GenerationSettings) error {
	settings.ID = models.SingletonID
	// Run inside a transaction so create-vs-update races resolve cleanly.
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing models.GenerationSettings
		if err := tx.First(&existing, models.SingletonID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return tx.Create(settings).Error
			}
			return err
		}
		return tx.Model(&existing).Updates(map[string]interface{}{
			"time_budget_sec": settings.TimeBudgetSec,
			"soft_weights":    settings.SoftWeights,
		}).Error
	})
}
