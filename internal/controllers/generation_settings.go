package controllers

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

// GenerationSettingsController exposes the admin-facing generation-settings
// API. This is a system-wide singleton: changes affect every future
// timetable generation run, hence the SuperAdminMiddleware gating in routes.
type GenerationSettingsController struct {
	repo repositories.GenerationSettingsRepository
}

func NewGenerationSettingsController(repo repositories.GenerationSettingsRepository) *GenerationSettingsController {
	return &GenerationSettingsController{repo: repo}
}

type UpdateGenerationSettingsRequest struct {
	TimeBudgetSec *float64            `json:"time_budget_sec"`
	SoftWeights   *map[string]float64 `json:"soft_weights"`
}

// Get handles GET .../generation-settings — returns current settings or
// populated defaults when never configured (never errors on empty table).
func (c *GenerationSettingsController) Get(ctx *gin.Context) {
	settings, err := c.repo.Get()
	if err != nil && err != repositories.ErrNotConfigured {
		logger.Error("Failed to load generation settings: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load generation settings"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"settings": settings})
}

// Update handles PUT .../generation-settings — strictly validates every
// field server-side:
//   - time_budget_sec must be > 0 and <= models.MaxTimeBudgetSec
//   - soft_weights keys must all be in models.AllowedSoftWeightKeys (unknown
//     keys are rejected with 400 listing the allowed keys, never silently
//     ignored)
//   - weight values must be finite and non-negative
func (c *GenerationSettingsController) Update(ctx *gin.Context) {
	var req UpdateGenerationSettingsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	settings, err := c.repo.Get()
	if err != nil && err != repositories.ErrNotConfigured {
		logger.Error("Failed to load generation settings: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load generation settings"})
		return
	}

	if req.TimeBudgetSec != nil {
		if err := validateTimeBudget(*req.TimeBudgetSec); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		settings.TimeBudgetSec = *req.TimeBudgetSec
	}

	if req.SoftWeights != nil {
		if err := validateSoftWeights(*req.SoftWeights); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		encoded, err := json.Marshal(*req.SoftWeights)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid soft_weights payload"})
			return
		}
		settings.SoftWeights = encoded
	}

	if err := c.repo.Upsert(settings); err != nil {
		logger.Error("Failed to save generation settings: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save generation settings"})
		return
	}

	saved, err := c.repo.Get()
	if err != nil && err != repositories.ErrNotConfigured {
		logger.Error("Failed to reload generation settings: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reload generation settings"})
		return
	}

	logger.Info("Generation settings updated: time_budget_sec=%.0f", saved.TimeBudgetSec)
	ctx.JSON(http.StatusOK, gin.H{"message": "Generation settings updated", "settings": saved})
}

func validateTimeBudget(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return settingsValidationError("time_budget_sec must be a finite number")
	}
	if v <= 0 {
		return settingsValidationError("time_budget_sec must be greater than 0")
	}
	if v > models.MaxTimeBudgetSec {
		return settingsValidationError("time_budget_sec must not exceed 300 seconds")
	}
	return nil
}

type settingsValidationError string

func (e settingsValidationError) Error() string { return string(e) }

func validateSoftWeights(weights map[string]float64) error {
	allowed := map[string]bool{}
	for _, k := range models.AllowedSoftWeightKeys {
		allowed[k] = true
	}
	for key, value := range weights {
		if !allowed[key] {
			return settingsValidationError("unknown soft_weights key: " + key +
				" (allowed keys: " + strings.Join(models.AllowedSoftWeightKeys, ", ") + ")")
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return settingsValidationError("soft_weights[" + key + "] must be a finite number")
		}
		if value < 0 {
			return settingsValidationError("soft_weights[" + key + "] must be >= 0")
		}
	}
	return nil
}
