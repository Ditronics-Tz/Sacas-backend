package controllers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
)

type TimetableController struct {
	timetableRepo   repositories.TimetableRepository
	timetableService *services.TimetableService
}

func NewTimetableController(
	timetableRepo repositories.TimetableRepository,
	timetableService *services.TimetableService,
) *TimetableController {
	return &TimetableController{
		timetableRepo:   timetableRepo,
		timetableService: timetableService,
	}
}

// CreateTimetableRequest represents the request payload for creating a timetable entry
type CreateTimetableRequest struct {
	ClassID   uint             `json:"class_id" binding:"required"`
	ModuleID  *uint            `json:"module_id"`
	SubjectID *uint            `json:"subject_id"`
	StaffID   uint             `json:"staff_id" binding:"required"`
	RoomID    uint             `json:"room_id" binding:"required"`
	Day       models.Weekday   `json:"day" binding:"required"`
	StartTime string           `json:"start_time" binding:"required"`
	EndTime   string           `json:"end_time" binding:"required"`
}

// UpdateTimetableRequest represents the request payload for updating a timetable entry
type UpdateTimetableRequest struct {
	ClassID   *uint            `json:"class_id"`
	ModuleID  *uint            `json:"module_id"`
	SubjectID *uint            `json:"subject_id"`
	StaffID   *uint            `json:"staff_id"`
	RoomID    *uint            `json:"room_id"`
	Day       *models.Weekday  `json:"day"`
	StartTime *string          `json:"start_time"`
	EndTime   *string          `json:"end_time"`
}

// GenerateTimetableRequest represents the request for generating a timetable
type GenerateTimetableRequest struct {
	ClassID uint `json:"class_id" binding:"required"`
}

// CreateTimetable creates a new timetable entry
func (c *TimetableController) CreateTimetable(ctx *gin.Context) {
	var req CreateTimetableRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid request payload: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	// Validate that either ModuleID or SubjectID is provided, but not both
	if (req.ModuleID == nil && req.SubjectID == nil) || (req.ModuleID != nil && req.SubjectID != nil) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Either module_id or subject_id must be provided, but not both"})
		return
	}

	timetable := &models.Timetable{
		ClassID:   req.ClassID,
		ModuleID:  req.ModuleID,
		SubjectID: req.SubjectID,
		StaffID:   req.StaffID,
		RoomID:    req.RoomID,
		Day:       req.Day,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	// Validate for conflicts
	if err := c.timetableService.ValidateTimeSlot(timetable); err != nil {
		logger.Error("Timetable validation failed: %v", err)
		ctx.JSON(http.StatusConflict, gin.H{"error": "Scheduling conflict detected", "details": err.Error()})
		return
	}

	if err := c.timetableRepo.Create(timetable); err != nil {
		logger.Error("Failed to create timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create timetable"})
		return
	}

	// Fetch the created timetable with relationships
	created, err := c.timetableRepo.GetByID(timetable.ID)
	if err != nil {
		logger.Error("Failed to fetch created timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch created timetable"})
		return
	}

	logger.Info("Timetable entry created successfully: ID %d", created.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Timetable entry created successfully", "timetable": created})
}

// GetTimetable gets a timetable entry by ID
func (c *TimetableController) GetTimetable(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timetable ID"})
		return
	}

	timetable, err := c.timetableRepo.GetByID(uint(id))
	if err != nil {
		logger.Error("Failed to get timetable: %v", err)
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Timetable entry not found"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"timetable": timetable})
}

// GetTimetableByClass gets timetable for a specific class
func (c *TimetableController) GetTimetableByClass(ctx *gin.Context) {
	classIDStr := ctx.Param("class_id")
	classID, err := strconv.ParseUint(classIDStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid class ID"})
		return
	}

	timetables, err := c.timetableRepo.GetByClass(uint(classID))
	if err != nil {
		logger.Error("Failed to get timetable for class: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get timetable"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"timetables": timetables})
}

// GetTimetableByStaff gets timetable for a specific staff member
func (c *TimetableController) GetTimetableByStaff(ctx *gin.Context) {
	staffIDStr := ctx.Param("staff_id")
	staffID, err := strconv.ParseUint(staffIDStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}

	timetables, err := c.timetableRepo.GetByStaff(uint(staffID))
	if err != nil {
		logger.Error("Failed to get timetable for staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get timetable"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"timetables": timetables})
}

// UpdateTimetable updates an existing timetable entry
func (c *TimetableController) UpdateTimetable(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timetable ID"})
		return
	}

	var req UpdateTimetableRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid request payload: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	// Get existing timetable
	timetable, err := c.timetableRepo.GetByID(uint(id))
	if err != nil {
		logger.Error("Failed to get timetable: %v", err)
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Timetable entry not found"})
		return
	}

	// Update fields if provided
	if req.ClassID != nil {
		timetable.ClassID = *req.ClassID
	}
	if req.ModuleID != nil {
		timetable.ModuleID = req.ModuleID
	}
	if req.SubjectID != nil {
		timetable.SubjectID = req.SubjectID
	}
	if req.StaffID != nil {
		timetable.StaffID = *req.StaffID
	}
	if req.RoomID != nil {
		timetable.RoomID = *req.RoomID
	}
	if req.Day != nil {
		timetable.Day = *req.Day
	}
	if req.StartTime != nil {
		timetable.StartTime = *req.StartTime
	}
	if req.EndTime != nil {
		timetable.EndTime = *req.EndTime
	}

	// Validate for conflicts
	if err := c.timetableService.ValidateTimeSlot(timetable); err != nil {
		logger.Error("Timetable validation failed: %v", err)
		ctx.JSON(http.StatusConflict, gin.H{"error": "Scheduling conflict detected", "details": err.Error()})
		return
	}

	if err := c.timetableRepo.Update(timetable); err != nil {
		logger.Error("Failed to update timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update timetable"})
		return
	}

	logger.Info("Timetable entry updated successfully: ID %d", timetable.ID)
	ctx.JSON(http.StatusOK, gin.H{"message": "Timetable entry updated successfully", "timetable": timetable})
}

// DeleteTimetable deletes a timetable entry
func (c *TimetableController) DeleteTimetable(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timetable ID"})
		return
	}

	if err := c.timetableRepo.Delete(uint(id)); err != nil {
		logger.Error("Failed to delete timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete timetable"})
		return
	}

	logger.Info("Timetable entry deleted successfully: ID %d", id)
	ctx.JSON(http.StatusOK, gin.H{"message": "Timetable entry deleted successfully"})
}

// GenerateTimetable generates a complete timetable for a class
func (c *TimetableController) GenerateTimetable(ctx *gin.Context) {
	var req GenerateTimetableRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid request payload: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	timetables, err := c.timetableService.GenerateTimetable(req.ClassID)
	if err != nil {
		logger.Error("Failed to generate timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate timetable", "details": err.Error()})
		return
	}

	logger.Info("Timetable generated successfully for class ID %d: %d entries", req.ClassID, len(timetables))
	ctx.JSON(http.StatusOK, gin.H{
		"message": "Timetable generated successfully",
		"timetables": timetables,
		"count": len(timetables),
	})
}

// ValidateTimetable validates a timetable for conflicts
func (c *TimetableController) ValidateTimetable(ctx *gin.Context) {
	// You can extend this to validate multiple timetable entries or specific constraints
	ctx.JSON(http.StatusOK, gin.H{"message": "Timetable validation endpoint - implement as needed"})
}