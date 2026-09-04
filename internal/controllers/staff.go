package controllers

import (
	"fmt"
	"net/http"
	"strconv"

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
}

func (c *StaffController) CreateStaff(ctx *gin.Context) {
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
	// If UserID is supplied, validate the User exists and is not already linked.
	if req.UserID != nil {
		if err := c.validateUserLink(nil, *req.UserID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
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
	}

	if req.Preferences != "" {
		staff.Preferences = []byte(req.Preferences)
	}

	if err := c.staffRepo.Create(staff); err != nil {
		logger.Error("Failed to create staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create staff"})
		return
	}

	created, err := c.staffRepo.GetByID(staff.ID)
	if err != nil {
		logger.Error("Failed to fetch created staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch created staff"})
		return
	}

	logger.Info("Staff created successfully: ID %d", created.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Staff created successfully", "staff": created})
}

func (c *StaffController) GetStaff(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}

	staff, err := c.staffRepo.GetByID(uint(id))
	if err != nil {
		logger.Error("Failed to get staff: %v", err)
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Staff not found"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"staff": staff})
}

func (c *StaffController) GetAllStaff(ctx *gin.Context) {
	limitStr := ctx.DefaultQuery("limit", "10")
	offsetStr := ctx.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 10
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	staff, err := c.staffRepo.GetAll(limit, offset)
	if err != nil {
		logger.Error("Failed to get staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get staff"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"staff": staff, "limit": limit, "offset": offset})
}

func (c *StaffController) UpdateStaff(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
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

	staff, err := c.staffRepo.GetByID(uint(id))
	if err != nil {
		logger.Error("Failed to get staff: %v", err)
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Staff not found"})
		return
	}

	if req.Name != nil {
		staff.Name = *req.Name
	}
	if req.Email != nil {
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
	if req.UserID != nil {
		if err := c.validateUserLink(&staff.ID, *req.UserID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		staff.UserID = req.UserID
	}

	if err := c.staffRepo.Update(staff); err != nil {
		logger.Error("Failed to update staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update staff"})
		return
	}

	logger.Info("Staff updated successfully: ID %d", staff.ID)
	ctx.JSON(http.StatusOK, gin.H{"message": "Staff updated successfully", "staff": staff})
}

func (c *StaffController) DeleteStaff(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}

	if err := c.staffRepo.Delete(uint(id)); err != nil {
		logger.Error("Failed to delete staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete staff"})
		return
	}

	logger.Info("Staff deleted successfully: ID %d", id)
	ctx.JSON(http.StatusOK, gin.H{"message": "Staff deleted successfully"})
}

// AssignModule POST /staff/:id/modules/:module_id
func (c *StaffController) AssignModule(ctx *gin.Context) {
	staffID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}
	moduleID, err := strconv.ParseUint(ctx.Param("module_id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	if _, err := c.staffRepo.GetByID(uint(staffID)); err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Staff not found"})
		return
	}
	if _, err := c.moduleRepo.GetByID(uint(moduleID)); err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Module not found"})
		return
	}

	if err := c.staffRepo.AssignModule(uint(staffID), uint(moduleID)); err != nil {
		logger.Error("Failed to assign module: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign module"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Module assigned to staff successfully"})
}

// UnassignModule DELETE /staff/:id/modules/:module_id
func (c *StaffController) UnassignModule(ctx *gin.Context) {
	staffID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}
	moduleID, err := strconv.ParseUint(ctx.Param("module_id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	if err := c.staffRepo.UnassignModule(uint(staffID), uint(moduleID)); err != nil {
		logger.Error("Failed to unassign module: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to unassign module"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Module unassigned from staff successfully"})
}

// ListStaffModules GET /staff/:id/modules
func (c *StaffController) ListStaffModules(ctx *gin.Context) {
	staffID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}

	if _, err := c.staffRepo.GetByID(uint(staffID)); err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Staff not found"})
		return
	}

	modules, err := c.staffRepo.ListModules(uint(staffID))
	if err != nil {
		logger.Error("Failed to list staff modules: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list modules"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"modules": modules})
}

// ListModuleStaff GET /modules/:id/staff
func (c *StaffController) ListModuleStaff(ctx *gin.Context) {
	moduleID, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	if _, err := c.moduleRepo.GetByID(uint(moduleID)); err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Module not found"})
		return
	}

	staff, err := c.staffRepo.ListStaffForModule(uint(moduleID))
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
func (c *StaffController) GetMyStaff(ctx *gin.Context) {
	rawUserID, exists := ctx.Get("user_id")
	if !exists || rawUserID == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}
	var userID uint
	switch v := rawUserID.(type) {
	case float64:
		userID = uint(v)
	case int:
		userID = uint(v)
	case uint:
		userID = v
	case int64:
		userID = uint(v)
	default:
		var n uint64
		if _, err := fmt.Sscanf(fmt.Sprint(v), "%d", &n); err != nil {
			ctx.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user in token"})
			return
		}
		userID = uint(n)
	}

	staff, err := c.staffRepo.GetByUserID(userID)
	if err != nil {
		// No linked staff — not a server error.
		ctx.JSON(http.StatusNotFound, gin.H{"error": "No staff profile is linked to your account", "staff": nil})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"staff": staff})
}

// validateUserLink ensures UserID exists and is not already linked to another staff.
func (c *StaffController) validateUserLink(currentStaffID *uint, userID uint) error {
	if c.userRepo != nil {
		if _, err := c.userRepo.GetByID(userID); err != nil {
			return fmt.Errorf("linked user_id %d does not exist", userID)
		}
	}
	// Enforce 1:1 — at most one staff per user. Check existing link.
	if existing, err := c.staffRepo.GetByUserID(userID); err == nil && existing != nil {
		if currentStaffID == nil || existing.ID != *currentStaffID {
			return fmt.Errorf("user_id %d is already linked to staff %d", userID, existing.ID)
		}
	}
	return nil
}
