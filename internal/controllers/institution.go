package controllers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/middlewares"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
)

// InstitutionController manages tenants. Every endpoint here is gated by
// RequirePlatformOnly in the router: only a platform super_admin (a super_admin
// with no institution_id) may list, create, or change institutions. A tenant
// admin can read only its own institution via /protected/institution/me.
type InstitutionController struct {
	repo  repositories.InstitutionRepository
	audit *services.AuditRecorder
}

// NewInstitutionController builds the controller. The audit recorder is required
// because every write here is a platform-level action on a tenant — the kind of
// event an operator will later need to explain.
func NewInstitutionController(repo repositories.InstitutionRepository, audit *services.AuditRecorder) *InstitutionController {
	return &InstitutionController{repo: repo, audit: audit}
}

type CreateInstitutionRequest struct {
	Name        string                   `json:"name" binding:"required,min=2,max=150"`
	Slug        string                   `json:"slug,omitempty" binding:"omitempty,min=2,max=150"`
	Type        models.InstitutionType   `json:"type" binding:"required"`
	Region      string                   `json:"region,omitempty"`
	Country     string                   `json:"country,omitempty"`
	Email       string                   `json:"email,omitempty" binding:"omitempty,email"`
	Phone       string                   `json:"phone,omitempty"`
	Address     string                   `json:"address,omitempty"`
	LogoURL     string                   `json:"logo_url,omitempty"`
	Status      models.InstitutionStatus `json:"status,omitempty"`
	Plan        models.InstitutionPlan   `json:"plan,omitempty"`
	TrialEndsAt *time.Time               `json:"trial_ends_at,omitempty"`
}

type UpdateInstitutionRequest struct {
	Name        *string                   `json:"name,omitempty" binding:"omitempty,min=2,max=150"`
	Slug        *string                   `json:"slug,omitempty" binding:"omitempty,min=2,max=150"`
	Type        *models.InstitutionType   `json:"type,omitempty"`
	Region      *string                   `json:"region,omitempty"`
	Country     *string                   `json:"country,omitempty"`
	Email       *string                   `json:"email,omitempty" binding:"omitempty,email"`
	Phone       *string                   `json:"phone,omitempty"`
	Address     *string                   `json:"address,omitempty"`
	LogoURL     *string                   `json:"logo_url,omitempty"`
	Status      *models.InstitutionStatus `json:"status,omitempty"`
	Plan        *models.InstitutionPlan   `json:"plan,omitempty"`
	TrialEndsAt *time.Time                `json:"trial_ends_at,omitempty"`
	ClearTrial  bool                      `json:"clear_trial,omitempty"`
}

func (c *InstitutionController) Create(ctx *gin.Context) {
	var req CreateInstitutionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}
	if !req.Type.IsValid() {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution type"})
		return
	}

	slug := req.Slug
	if slug == "" {
		slug = middlewares.Slugify(req.Name)
	}
	if slug == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Could not derive a slug from name; provide slug explicitly"})
		return
	}
	if existing, err := c.repo.GetBySlug(slug); err == nil && existing != nil {
		ctx.JSON(http.StatusConflict, gin.H{"error": "An institution with this slug already exists"})
		return
	}

	// New institutions start pending: they must be explicitly activated
	// before anyone at that campus can sign in.
	status := req.Status
	if status == "" {
		status = models.InstitutionStatusPending
	}
	if !status.IsValid() {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution status"})
		return
	}
	plan := req.Plan
	if plan == "" {
		plan = models.InstitutionPlanFree
	}
	if !plan.IsValid() {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution plan"})
		return
	}
	country := req.Country
	if country == "" {
		country = "Tanzania"
	}

	institution := &models.Institution{
		Name:        req.Name,
		Slug:        slug,
		Type:        req.Type,
		Region:      req.Region,
		Country:     country,
		Email:       req.Email,
		Phone:       req.Phone,
		Address:     req.Address,
		LogoURL:     req.LogoURL,
		Status:      status,
		Plan:        plan,
		TrialEndsAt: req.TrialEndsAt,
	}

	if err := c.repo.Create(institution); err != nil {
		logger.Error("Failed to create institution: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create institution"})
		return
	}

	logger.Info("Institution created: %s (ID %d, status %s)", institution.Name, institution.ID, institution.Status)
	c.audit.Record(ctx, models.AuditInstitutionCreate, "institution",
		uintString(institution.ID), institution.Name, map[string]any{
			"status": string(institution.Status),
			"plan":   string(institution.Plan),
			"type":   string(institution.Type),
		})
	ctx.JSON(http.StatusCreated, gin.H{"message": "Institution created successfully", "institution": institution})
}

func (c *InstitutionController) Get(ctx *gin.Context) {
	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution ID"})
		return
	}

	institution, err := c.repo.GetByID(id)
	if err != nil {
		respondRepoError(ctx, "Institution not found", err)
		return
	}

	userCount, err := c.repo.CountUsers(id)
	if err != nil {
		logger.Warn("Failed to count users for institution %d: %v", id, err)
	}

	ctx.JSON(http.StatusOK, gin.H{"institution": institution, "user_count": userCount})
}

func (c *InstitutionController) GetAll(ctx *gin.Context) {
	limit, offset := parsePagination(ctx)
	status := ctx.Query("status")

	var (
		institutions []models.Institution
		err          error
	)
	if status != "" {
		if !models.InstitutionStatus(status).IsValid() {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status filter"})
			return
		}
		institutions, err = c.repo.GetByStatus(models.InstitutionStatus(status), limit, offset)
	} else {
		institutions, err = c.repo.GetAll(limit, offset)
	}
	if err != nil {
		logger.Error("Failed to list institutions: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list institutions"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"institutions": institutions, "limit": limit, "offset": offset})
}

func (c *InstitutionController) Update(ctx *gin.Context) {
	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution ID"})
		return
	}

	institution, err := c.repo.GetByID(id)
	if err != nil {
		respondRepoError(ctx, "Institution not found", err)
		return
	}

	var req UpdateInstitutionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	// Capture the state before the change so the trail records the transition,
	// not just the destination. A suspension in particular is the event an
	// operator will be asked about later.
	previousStatus := institution.Status
	previousPlan := institution.Plan
	previousTrial := institution.TrialEndsAt

	if req.Name != nil {
		institution.Name = *req.Name
	}
	if req.Slug != nil {
		if existing, err := c.repo.GetBySlug(*req.Slug); err == nil && existing != nil && existing.ID != id {
			ctx.JSON(http.StatusConflict, gin.H{"error": "An institution with this slug already exists"})
			return
		}
		institution.Slug = *req.Slug
	}
	if req.Type != nil {
		if !req.Type.IsValid() {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution type"})
			return
		}
		institution.Type = *req.Type
	}
	if req.Region != nil {
		institution.Region = *req.Region
	}
	if req.Country != nil {
		institution.Country = *req.Country
	}
	if req.Email != nil {
		institution.Email = *req.Email
	}
	if req.Phone != nil {
		institution.Phone = *req.Phone
	}
	if req.Address != nil {
		institution.Address = *req.Address
	}
	if req.LogoURL != nil {
		institution.LogoURL = *req.LogoURL
	}
	if req.Status != nil {
		if !req.Status.IsValid() {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution status"})
			return
		}
		institution.Status = *req.Status
	}
	if req.Plan != nil {
		if !req.Plan.IsValid() {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution plan"})
			return
		}
		institution.Plan = *req.Plan
	}
	switch {
	case req.ClearTrial:
		institution.TrialEndsAt = nil
	case req.TrialEndsAt != nil:
		institution.TrialEndsAt = req.TrialEndsAt
	}

	if err := c.repo.Update(institution); err != nil {
		logger.Error("Failed to update institution %d: %v", id, err)
		respondRepoError(ctx, "Institution not found", err)
		return
	}

	logger.Info("Institution updated: %s (ID %d, status %s)", institution.Name, institution.ID, institution.Status)

	// A status change is audited as a suspension when the institution stopped
	// being usable, because that is the question the trail gets asked.
	if institution.Status != previousStatus {
		action := models.AuditInstitutionUpdate
		if institution.Status == models.InstitutionStatusSuspended {
			action = models.AuditInstitutionSuspend
		}
		c.audit.Record(ctx, action, "institution", uintString(institution.ID), institution.Name, map[string]any{
			"from": string(previousStatus),
			"to":   string(institution.Status),
		})
	}
	if institution.Plan != previousPlan {
		c.audit.Record(ctx, models.AuditInstitutionPlanChange, "institution", uintString(institution.ID), institution.Name, map[string]any{
			"from": string(previousPlan),
			"to":   string(institution.Plan),
		})
	}
	if trialChanged(previousTrial, institution.TrialEndsAt) {
		c.audit.Record(ctx, models.AuditInstitutionUpdate, "institution", uintString(institution.ID), institution.Name, map[string]any{
			"trial_ends_at": institution.TrialEndsAt,
			"trial_changed": true,
		})
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Institution updated successfully", "institution": institution})
}

// trialChanged reports whether a trial end date actually moved.
func trialChanged(before, after *time.Time) bool {
	switch {
	case before == nil && after == nil:
		return false
	case before == nil || after == nil:
		return true
	default:
		return !before.Equal(*after)
	}
}

func (c *InstitutionController) Delete(ctx *gin.Context) {
	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution ID"})
		return
	}

	// The default institution is the backfill target for existing data and the
	// implicit fallback in the demo seed. Deleting it would orphan every
	// record that was not explicitly moved.
	if id == models.DefaultInstitutionID {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "The default institution cannot be deleted",
			"details": "Suspend it instead, or move its data to another institution first",
		})
		return
	}

	if err := c.repo.Delete(id); err != nil {
		logger.Error("Failed to delete institution %d: %v", id, err)
		respondRepoError(ctx, "Institution not found", err)
		return
	}

	logger.Info("Institution deleted: ID %d", id)
	c.audit.Record(ctx, models.AuditInstitutionDelete, "institution", uintString(id), "", map[string]any{
		"severity": "high",
	})
	ctx.JSON(http.StatusOK, gin.H{"message": "Institution deleted successfully"})
}

// GetMe handles GET /protected/institution/me — the signed-in tenant's own
// profile. The institution ID comes from the resolved session, never from the
// request, so a user can only ever read their own institution.
func (c *InstitutionController) GetMe(ctx *gin.Context) {
	inst := tenantID(ctx)

	// A platform super_admin has no single institution; report that plainly
	// rather than 404.
	if repositories.IsPlatformScope(inst) {
		ctx.JSON(http.StatusOK, gin.H{
			"institution": nil,
			"scope":       "platform",
			"message":     "This account is a platform administrator and is not bound to a single institution",
		})
		return
	}

	institution, err := c.repo.GetByID(inst)
	if err != nil {
		respondRepoError(ctx, "Institution not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"institution": institution,
		"scope":       "institution",
	})
}
