package controllers

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

// GenerationSettingsController exposes the admin-facing generation-settings
// API. Settings are layered: a platform-wide default row plus an optional
// per-institution override. An institution admin sees and edits only their own
// override; a platform super_admin sees the default and may also target an
// institution.
type GenerationSettingsController struct {
	repo repositories.GenerationSettingsRepository
}

func NewGenerationSettingsController(repo repositories.GenerationSettingsRepository) *GenerationSettingsController {
	return &GenerationSettingsController{repo: repo}
}

type UpdateGenerationSettingsRequest struct {
	TimeBudgetSec *float64            `json:"time_budget_sec"`
	SoftWeights   *map[string]float64 `json:"soft_weights"`
	// InstitutionID targets a specific institution's override. Only a platform
	// super_admin may set it; anyone else is pinned to their own institution.
	InstitutionID *uint `json:"institution_id,omitempty"`
	// InheritPlatform drops this institution's override so it goes back to
	// inheriting the platform default.
	InheritPlatform bool `json:"inherit_platform,omitempty"`
}

// targetInstitution resolves which settings row the request applies to, and
// returns an error when the caller is not allowed to touch it.
func (c *GenerationSettingsController) targetInstitution(ctx *gin.Context, requested *uint) (uint, error) {
	caller := tenantID(ctx)
	isPlatform := repositories.IsPlatformScope(caller) &&
		ctx.GetString("role") == string(models.RoleSuperAdmin)

	if !isPlatform {
		if requested != nil && *requested != caller {
			return 0, errSettingsForbidden
		}
		return caller, nil
	}
	// Platform super_admin: no target means the platform default row.
	if requested == nil {
		return repositories.PlatformScope, nil
	}
	return *requested, nil
}

var errSettingsForbidden = settingsValidationError("Platform administrator access required")

// Get handles GET .../generation-settings — returns the effective settings
// (institution override, else platform default, else built-in defaults) so the
// endpoint never errors on an empty table.
func (c *GenerationSettingsController) Get(ctx *gin.Context) {
	target, err := c.targetInstitution(ctx, queryInstitutionID(ctx))
	if err != nil {
		ctx.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	settings, err := c.repo.GetForInstitution(target)
	if err != nil && err != repositories.ErrNotConfigured {
		logger.Error("Failed to load generation settings: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load generation settings"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"settings":        settings,
		"institution_id":  target,
		"is_platform_row": repositories.IsPlatformScope(target),
	})
}

// queryInstitutionID reads an optional ?institution_id= target.
func queryInstitutionID(ctx *gin.Context) *uint {
	raw := ctx.Query("institution_id")
	if raw == "" {
		return nil
	}
	parsed, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return nil
	}
	id := uint(parsed)
	return &id
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

	target, err := c.targetInstitution(ctx, req.InstitutionID)
	if err != nil {
		ctx.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	if req.InheritPlatform {
		if err := c.repo.DeleteOverride(target); err != nil {
			logger.Error("Failed to clear generation settings override: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save generation settings"})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"message": "Institution now inherits the platform generation settings"})
		return
	}

	settings, err := c.repo.GetForInstitution(target)
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

	if err := c.repo.UpsertForInstitution(target, settings); err != nil {
		logger.Error("Failed to save generation settings: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save generation settings"})
		return
	}

	saved, err := c.repo.GetForInstitution(target)
	if err != nil && err != repositories.ErrNotConfigured {
		logger.Error("Failed to reload generation settings: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reload generation settings"})
		return
	}

	logger.Info("Generation settings updated (institution %d): time_budget_sec=%.0f", target, saved.TimeBudgetSec)
	ctx.JSON(http.StatusOK, gin.H{
		"message":        "Generation settings updated",
		"settings":       saved,
		"institution_id": target,
	})
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
