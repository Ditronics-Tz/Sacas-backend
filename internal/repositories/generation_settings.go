package repositories

import (
	"errors"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// ErrNotConfigured is returned when no settings row exists at all (fresh
// deployment). It is deliberately distinct from a database error so callers
// (buildSolverRequest in ticket 136.c) can fall back to defaults on
// ErrNotConfigured while still failing loudly on a real DB failure.
var ErrNotConfigured = errors.New("generation settings not configured")

type GenerationSettingsRepository interface {
	// Get returns the platform default row. When no row exists yet it returns
	// populated defaults AND ErrNotConfigured (not a hard failure).
	Get() (*models.GenerationSettings, error)

	// GetForInstitution resolves the effective settings for an institution:
	// its own override row if one exists, otherwise the platform default,
	// otherwise built-in defaults. ErrNotConfigured accompanies the latter so
	// callers can still tell "nothing configured" from "using defaults".
	GetForInstitution(institutionID uint) (*models.GenerationSettings, error)

	// GetOverride returns an institution's own override row, or nil when it
	// has none. It does not fall back to the platform row.
	GetOverride(institutionID uint) (*models.GenerationSettings, error)

	// Upsert creates or updates the platform default row (fixed primary key).
	Upsert(settings *models.GenerationSettings) error

	// UpsertForInstitution creates or updates an institution's override row.
	UpsertForInstitution(institutionID uint, settings *models.GenerationSettings) error

	// DeleteOverride removes an institution's override so it inherits the
	// platform default again.
	DeleteOverride(institutionID uint) error
}

type generationSettingsRepository struct {
	db *gorm.DB
}

func NewGenerationSettingsRepository(db *gorm.DB) GenerationSettingsRepository {
	return &generationSettingsRepository{db: db}
}

func (r *generationSettingsRepository) Get() (*models.GenerationSettings, error) {
	var settings models.GenerationSettings
	err := r.db.Where("institution_id = ?", models.PlatformScope).
		First(&settings, models.SingletonID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Populated defaults + sentinel: callers choose to fall back or fail.
		return models.DefaultGenerationSettings(), ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (r *generationSettingsRepository) GetOverride(institutionID uint) (*models.GenerationSettings, error) {
	if IsPlatformScope(institutionID) {
		// The platform row is not an override.
		return nil, nil
	}
	var settings models.GenerationSettings
	err := r.db.Where("institution_id = ?", institutionID).First(&settings).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

// GetForInstitution layers the institution's override on top of the platform
// default, so an institution that only overrides the time budget keeps the
// platform's soft weights.
func (r *generationSettingsRepository) GetForInstitution(institutionID uint) (*models.GenerationSettings, error) {
	platform, platformErr := r.Get()
	// A real database failure is fatal; only ErrNotConfigured is tolerable.
	if platformErr != nil && !errors.Is(platformErr, ErrNotConfigured) {
		return nil, platformErr
	}

	override, err := r.GetOverride(institutionID)
	if err != nil {
		return nil, err
	}
	if override == nil {
		if platformErr != nil {
			return platform, platformErr
		}
		return platform, nil
	}
	// The override row is complete on its own fields, so a straight swap is
	// enough; TimeBudgetSec is only inherited when the override left it unset.
	if override.TimeBudgetSec <= 0 {
		override.TimeBudgetSec = platform.TimeBudgetSec
	}
	if len(override.SoftWeights) == 0 {
		override.SoftWeights = platform.SoftWeights
	}
	return override, nil
}

func (r *generationSettingsRepository) Upsert(settings *models.GenerationSettings) error {
	if settings == nil {
		return gorm.ErrInvalidValue
	}
	settings.ID = models.SingletonID
	settings.InstitutionID = models.PlatformScope
	// Run inside a transaction so create-vs-update races resolve cleanly.
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing models.GenerationSettings
		if err := tx.Where("institution_id = ?", models.PlatformScope).
			First(&existing, models.SingletonID).Error; err != nil {
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

func (r *generationSettingsRepository) UpsertForInstitution(institutionID uint, settings *models.GenerationSettings) error {
	if settings == nil {
		return gorm.ErrInvalidValue
	}
	if IsPlatformScope(institutionID) {
		// Writing with no institution means writing the platform default.
		return r.Upsert(settings)
	}
	settings.InstitutionID = institutionID
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing models.GenerationSettings
		if err := tx.Where("institution_id = ?", institutionID).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				settings.ID = 0
				return tx.Create(settings).Error
			}
			return err
		}
		settings.ID = existing.ID
		return tx.Model(&existing).Updates(map[string]interface{}{
			"time_budget_sec": settings.TimeBudgetSec,
			"soft_weights":    settings.SoftWeights,
		}).Error
	})
}

func (r *generationSettingsRepository) DeleteOverride(institutionID uint) error {
	if IsPlatformScope(institutionID) {
		return gorm.ErrInvalidValue
	}
	return r.db.Where("institution_id = ?", institutionID).
		Delete(&models.GenerationSettings{}).Error
}
