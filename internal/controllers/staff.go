package controllers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

type StaffController struct {
	staffRepo repositories.StaffRepository
}

func NewStaffController(staffRepo repositories.StaffRepository) *StaffController {
	return &StaffController{
		staffRepo: staffRepo,
	}
}

// CreateStaffRequest represents the request payload for creating staff
type CreateStaffRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=100"`
	Email       string `json:"email" binding:"required,email"`
	FacultyID   uint   `json:"faculty_id" binding:"required"`
	MaxHours    int    `json:"max_hours" binding:"min=1,max=60"`
	Preferences string `json:"preferences,omitempty"`
}

// UpdateStaffRequest represents the request payload for updating staff
type UpdateStaffRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=2,max=100"`
	Email       *string `json:"email,omitempty" binding:"omitempty,email"`
	FacultyID   *uint   `json:"faculty_id,omitempty"`
	MaxHours    *int    `json:"max_hours,omitempty" binding:"omitempty,min=1,max=60"`
	Preferences *string `json:"preferences,omitempty"`
}

// CreateStaff creates a new staff member
func (c *StaffController) CreateStaff(ctx *gin.Context) {
	var req CreateStaffRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid request payload: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	if req.MaxHours == 0 {
		req.MaxHours = 40 // Default value
	}

	staff := &models.Staff{
		Name:      req.Name,
		Email:     req.Email,
		FacultyID: req.FacultyID,
		MaxHours:  req.MaxHours,
	}

	if req.Preferences != "" {
		// Convert preferences string to JSONB (simplified - should validate JSON)
		staff.Preferences = []byte(req.Preferences)
	}

	if err := c.staffRepo.Create(staff); err != nil {
		logger.Error("Failed to create staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create staff"})
		return
	}

	// Fetch created staff with relationships
	created, err := c.staffRepo.GetByID(staff.ID)
	if err != nil {
		logger.Error("Failed to fetch created staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch created staff"})
		return
	}

	logger.Info("Staff created successfully: ID %d", created.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Staff created successfully", "staff": created})
}

// GetStaff gets a staff member by ID
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

// GetAllStaff gets all staff members with pagination
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

// UpdateStaff updates an existing staff member
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

	// Get existing staff
	staff, err := c.staffRepo.GetByID(uint(id))
	if err != nil {
		logger.Error("Failed to get staff: %v", err)
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Staff not found"})
		return
	}

	// Update fields if provided
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

	if err := c.staffRepo.Update(staff); err != nil {
		logger.Error("Failed to update staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update staff"})
		return
	}

	logger.Info("Staff updated successfully: ID %d", staff.ID)
	ctx.JSON(http.StatusOK, gin.H{"message": "Staff updated successfully", "staff": staff})
}

// DeleteStaff deletes a staff member
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