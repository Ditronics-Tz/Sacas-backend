package controllers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

type StaffController struct {
	staffRepo  repositories.StaffRepository
	moduleRepo repositories.ModuleRepository
	userRepo   repositories.UserRepository
}

func NewStaffController(staffRepo repositories.StaffRepository, moduleRepo repositories.ModuleRepository) *StaffController {
	return &StaffController{
		staffRepo:  staffRepo,
		moduleRepo: moduleRepo,
	}
}

// NewStaffControllerWithUser is used in production wiring to allow UserID validation.
func NewStaffControllerWithUser(staffRepo repositories.StaffRepository, moduleRepo repositories.ModuleRepository, userRepo repositories.UserRepository) *StaffController {
	return &StaffController{
		staffRepo:  staffRepo,
		moduleRepo: moduleRepo,
		userRepo:   userRepo,
	}
}

type CreateStaffRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=100"`
	Email       string `json:"email" binding:"required,email"`
	FacultyID   uint   `json:"faculty_id" binding:"required"`
	MaxHours    int    `json:"max_hours" binding:"omitempty,min=1,max=60"`
	Preferences string `json:"preferences,omitempty"`
	RfidID      string `json:"rfid_id,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Title       string `json:"title,omitempty"`
	StaffType   string `json:"staff_type,omitempty"`
	// UserID optionally links this staff record to an existing login account
	// (User). Admin-only field; this is the trusted User→Staff mapping used by
	// GET /protected/timetable/my. Unique constraint prevents double-linking.
	UserID *uint `json:"user_id,omitempty"`
}

type UpdateStaffRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=2,max=100"`
	Email       *string `json:"email,omitempty" binding:"omitempty,email"`
	FacultyID   *uint   `json:"faculty_id,omitempty"`
	MaxHours    *int    `json:"max_hours,omitempty" binding:"omitempty,min=1,max=60"`
	Preferences *string `json:"preferences,omitempty"`
	RfidID      *string `json:"rfid_id,omitempty"`
	PhoneNumber *string `json:"phone_number,omitempty"`
	Title       *string `json:"title,omitempty"`
	StaffType   *string `json:"staff_type,omitempty"`
	// UserID links/relinks this staff record to a login account (admin-only).
	UserID *uint `json:"user_id,omitempty"`
	// ClearUserID unlinks the staff record from its login account.
	ClearUserID bool `json:"clear_user_id,omitempty"`
}

func (c *StaffController) CreateStaff(ctx *gin.Context) {
	inst := tenantID(ctx)

	var req CreateStaffRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid request payload: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	if req.MaxHours == 0 {
		req.MaxHours = 40
	}

	// Explicit UserID linking only — never auto-match by email.
	// If UserID is supplied, validate the User exists, belongs to this
	// institution, and is not already linked to another staff record.
	if req.UserID != nil {
		if err := c.validateUserLink(inst, nil, *req.UserID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	// Email is unique per institution, not globally. Report the collision
	// here rather than letting it surface as an opaque 500.
	if existing, err := c.staffRepo.GetByEmail(inst, req.Email); err == nil && existing != nil {
		ctx.JSON(http.StatusConflict, gin.H{"error": "A staff member with this email already exists in this institution"})
		return
	}

	staff := &models.Staff{
		Name:        req.Name,
		Email:       req.Email,
		FacultyID:   req.FacultyID,
		MaxHours:    req.MaxHours,
		RfidID:      req.RfidID,
		PhoneNumber: req.PhoneNumber,
		Title:       req.Title,
		StaffType:   req.StaffType,
		UserID:      req.UserID,
		// Stamped from the session, never from the payload.
		InstitutionID: inst,
	}

	if req.Preferences != "" {
		staff.Preferences = []byte(req.Preferences)
	}

	if err := c.staffRepo.Create(inst, staff); err != nil {
		logger.Error("Failed to create staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create staff"})
		return
	}

	created, err := c.staffRepo.GetByID(inst, staff.ID)
	if err != nil {
		logger.Error("Failed to fetch created staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch created staff"})
		return
	}

	logger.Info("Staff created successfully: ID %d", created.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Staff created successfully", "staff": created})
}

func (c *StaffController) GetStaff(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}

	staff, err := c.staffRepo.GetByID(inst, id)
	if err != nil {
		logger.Error("Failed to get staff: %v", err)
		respondRepoError(ctx, "Staff not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"staff": staff})
}

func (c *StaffController) GetAllStaff(ctx *gin.Context) {
	inst := tenantID(ctx)

	limit, offset := parsePagination(ctx)

	staff, err := c.staffRepo.GetAll(inst, limit, offset)
	if err != nil {
		logger.Error("Failed to get staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get staff"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"staff": staff, "limit": limit, "offset": offset})
}

func (c *StaffController) UpdateStaff(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}

	var req UpdateStaffRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid request payload: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	// A staff record owned by another institution resolves to not-found, so
	// the update below can never reach it.
	staff, err := c.staffRepo.GetByID(inst, id)
	if err != nil {
		logger.Error("Failed to get staff: %v", err)
		respondRepoError(ctx, "Staff not found", err)
		return
	}

	if req.Name != nil {
		staff.Name = *req.Name
	}
	if req.Email != nil {
		if existing, err := c.staffRepo.GetByEmail(inst, *req.Email); err == nil && existing != nil && existing.ID != staff.ID {
			ctx.JSON(http.StatusConflict, gin.H{"error": "A staff member with this email already exists in this institution"})
			return
		}
		staff.Email = *req.Email
	}
	if req.FacultyID != nil {
		staff.FacultyID = *req.FacultyID
	}
	if req.MaxHours != nil {
		staff.MaxHours = *req.MaxHours
	}
	if req.Preferences != nil {
		staff.Preferences = []byte(*req.Preferences)
	}
	if req.RfidID != nil {
		staff.RfidID = *req.RfidID
	}
	if req.PhoneNumber != nil {
		staff.PhoneNumber = *req.PhoneNumber
	}
	if req.Title != nil {
		staff.Title = *req.Title
	}
	if req.StaffType != nil {
		staff.StaffType = *req.StaffType
	}

	if err := c.staffRepo.Update(inst, staff); err != nil {
		logger.Error("Failed to update staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update staff"})
		return
	}

	// The login link is written separately: it is a privileged field and must
	// not be settable through the ordinary profile update above.
	switch {
	case req.ClearUserID:
		if err := c.staffRepo.SetUserLink(inst, staff.ID, nil); err != nil {
			logger.Error("Failed to clear staff user link: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update staff"})
			return
		}
		staff.UserID = nil
	case req.UserID != nil:
		if err := c.validateUserLink(inst, &staff.ID, *req.UserID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := c.staffRepo.SetUserLink(inst, staff.ID, req.UserID); err != nil {
			logger.Error("Failed to link staff user: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update staff"})
			return
		}
		staff.UserID = req.UserID
	}

	logger.Info("Staff updated successfully: ID %d", staff.ID)
	ctx.JSON(http.StatusOK, gin.H{"message": "Staff updated successfully", "staff": staff})
}

func (c *StaffController) DeleteStaff(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}

	if err := c.staffRepo.Delete(inst, id); err != nil {
		logger.Error("Failed to delete staff: %v", err)
		respondRepoError(ctx, "Staff not found", err)
		return
	}

	logger.Info("Staff deleted successfully: ID %d", id)
	ctx.JSON(http.StatusOK, gin.H{"message": "Staff deleted successfully"})
}

// AssignModule POST /staff/:id/modules/:module_id
func (c *StaffController) AssignModule(ctx *gin.Context) {
	inst := tenantID(ctx)

	staffID, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}
	moduleID, err := parseIDParam(ctx, "module_id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	// Both sides are resolved inside the institution, so a cross-tenant staff
	// or module ID is a 404 rather than a link that spans two campuses.
	if _, err := c.staffRepo.GetByID(inst, staffID); err != nil {
		respondRepoError(ctx, "Staff not found", err)
		return
	}
	if _, err := c.moduleRepo.GetByID(inst, moduleID); err != nil {
		respondRepoError(ctx, "Module not found", err)
		return
	}

	if err := c.staffRepo.AssignModule(inst, staffID, moduleID); err != nil {
		logger.Error("Failed to assign module: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign module"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Module assigned to staff successfully"})
}

// UnassignModule DELETE /staff/:id/modules/:module_id
func (c *StaffController) UnassignModule(ctx *gin.Context) {
	inst := tenantID(ctx)

	staffID, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}
	moduleID, err := parseIDParam(ctx, "module_id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	if err := c.staffRepo.UnassignModule(inst, staffID, moduleID); err != nil {
		logger.Error("Failed to unassign module: %v", err)
		respondRepoError(ctx, "Staff not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Module unassigned from staff successfully"})
}

// ListStaffModules GET /staff/:id/modules
func (c *StaffController) ListStaffModules(ctx *gin.Context) {
	inst := tenantID(ctx)

	staffID, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}

	if _, err := c.staffRepo.GetByID(inst, staffID); err != nil {
		respondRepoError(ctx, "Staff not found", err)
		return
	}

	modules, err := c.staffRepo.ListModules(inst, staffID)
	if err != nil {
		logger.Error("Failed to list staff modules: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list modules"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"modules": modules})
}

// ListModuleStaff GET /modules/:id/staff
func (c *StaffController) ListModuleStaff(ctx *gin.Context) {
	inst := tenantID(ctx)

	moduleID, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	if _, err := c.moduleRepo.GetByID(inst, moduleID); err != nil {
		respondRepoError(ctx, "Module not found", err)
		return
	}

	staff, err := c.staffRepo.ListStaffForModule(inst, moduleID)
	if err != nil {
		logger.Error("Failed to list module staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list staff"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"staff": staff})
}

// GetMyStaff handles GET /api/protected/me/staff — returns the Staff record
// linked to the currently authenticated user via staff.user_id FK.
// Spec: user with no linked staff gets 404 (clear, not 500); linked user gets staff.
//
// Both the user ID and the institution come from the resolved session, never
// from the request, so a user can only ever read their own staff record.
func (c *StaffController) GetMyStaff(ctx *gin.Context) {
	inst := tenantID(ctx)

	userID, ok := currentUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	staff, err := c.staffRepo.GetByUserID(inst, userID)
	if err != nil {
		// No linked staff — not a server error.
		ctx.JSON(http.StatusNotFound, gin.H{"error": "No staff profile is linked to your account", "staff": nil})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"staff": staff})
}

// validateUserLink ensures UserID exists, belongs to the caller's institution,
// and is not already linked to another staff record.
func (c *StaffController) validateUserLink(institutionID uint, currentStaffID *uint, userID uint) error {
	if c.userRepo != nil {
		user, err := c.userRepo.GetByID(userID)
		if err != nil {
			return fmt.Errorf("linked user_id %d does not exist", userID)
		}
		// Refuse to link across institutions: that would let one campus's
		// timetable surface in another campus's staff view.
		if user.InstitutionID == nil || *user.InstitutionID != institutionID {
			return fmt.Errorf("linked user_id %d does not belong to this institution", userID)
		}
	}
	// Enforce 1:1 — at most one staff per user. Check existing link.
	if existing, err := c.staffRepo.GetByUserID(institutionID, userID); err == nil && existing != nil {
		if currentStaffID == nil || existing.ID != *currentStaffID {
			return fmt.Errorf("user_id %d is already linked to staff %d", userID, existing.ID)
		}
	}
	return nil
}
