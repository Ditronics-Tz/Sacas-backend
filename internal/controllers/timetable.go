package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
)

type TimetableController struct {
	timetableRepo    repositories.TimetableRepository
	timetableService *services.TimetableService
}

func NewTimetableController(
	timetableRepo repositories.TimetableRepository,
	timetableService *services.TimetableService,
) *TimetableController {
	return &TimetableController{
		timetableRepo:    timetableRepo,
		timetableService: timetableService,
	}
}

type CreateTimetableRequest struct {
	ClassID   uint           `json:"class_id" binding:"required"`
	ModuleID  *uint          `json:"module_id"`
	SubjectID *uint          `json:"subject_id"`
	StaffID   uint           `json:"staff_id" binding:"required"`
	RoomID    uint           `json:"room_id" binding:"required"`
	Day       models.Weekday `json:"day" binding:"required"`
	StartTime string         `json:"start_time" binding:"required"`
	EndTime   string         `json:"end_time" binding:"required"`
}

type UpdateTimetableRequest struct {
	ClassID   *uint           `json:"class_id"`
	ModuleID  *uint           `json:"module_id"`
	SubjectID *uint           `json:"subject_id"`
	StaffID   *uint           `json:"staff_id"`
	RoomID    *uint           `json:"room_id"`
	Day       *models.Weekday `json:"day"`
	StartTime *string         `json:"start_time"`
	EndTime   *string         `json:"end_time"`
}

type GenerateTimetableRequest struct {
	ClassID uint `json:"class_id" binding:"required"`
}

func (c *TimetableController) CreateTimetable(ctx *gin.Context) {
	var req CreateTimetableRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid request payload: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

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

	if err := c.timetableService.ValidateTimeSlot(timetable, 0); err != nil {
		logger.Error("Timetable validation failed: %v", err)
		ctx.JSON(http.StatusConflict, gin.H{"error": "Scheduling conflict detected", "details": err.Error()})
		return
	}

	if err := c.timetableRepo.Create(timetable); err != nil {
		logger.Error("Failed to create timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create timetable"})
		return
	}

	created, err := c.timetableRepo.GetByID(timetable.ID)
	if err != nil {
		logger.Error("Failed to fetch created timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch created timetable"})
		return
	}

	logger.Info("Timetable entry created successfully: ID %d", created.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Timetable entry created successfully", "timetable": created})
}

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

func (c *TimetableController) GetTimetableByStaff(ctx *gin.Context) {
	// Route is /by-staff/:staff_id to avoid clashing with staff CRUD /staff/:id
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

	timetable, err := c.timetableRepo.GetByID(uint(id))
	if err != nil {
		logger.Error("Failed to get timetable: %v", err)
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Timetable entry not found"})
		return
	}

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

	if err := c.timetableService.ValidateTimeSlot(timetable, uint(id)); err != nil {
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

func (c *TimetableController) GenerateTimetable(ctx *gin.Context) {
	var req GenerateTimetableRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid request payload: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	result, err := c.timetableService.GenerateTimetable(req.ClassID)
	if err != nil {
		logger.Error("Failed to generate timetable: %v", err)
		status := http.StatusInternalServerError
		body := gin.H{"error": "Failed to generate timetable", "details": err.Error()}
		if result != nil {
			body["status"] = result.Status
			body["unsat_reasons"] = result.UnsatReasons
			body["engine"] = result.Engine
			body["required_sessions"] = result.RequiredSessions
			body["scheduled_sessions"] = result.ScheduledSessions
			if result.Status == "infeasible" || result.Status == "partial" {
				status = http.StatusUnprocessableEntity
			}
		}
		if strings.Contains(err.Error(), "infeasible") {
			status = http.StatusUnprocessableEntity
		}
		ctx.JSON(status, body)
		return
	}

	logger.Info("Timetable generated for class %d: %d entries via %s", req.ClassID, len(result.Timetables), result.Engine)
	ctx.JSON(http.StatusOK, gin.H{
		"message":                   "Timetable generated successfully (replaced previous class slots)",
		"timetables":                result.Timetables,
		"count":                     len(result.Timetables),
		"status":                    result.Status,
		"violated_soft_constraints": result.ViolatedSoftConstraints,
		"engine":                    result.Engine,
		"required_sessions":         result.RequiredSessions,
		"scheduled_sessions":        result.ScheduledSessions,
	})
}

func (c *TimetableController) PreviewGenerateTimetable(ctx *gin.Context) {
	var req GenerateTimetableRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	result, err := c.timetableService.PreviewTimetable(req.ClassID)
	if err != nil {
		logger.Error("Failed to preview timetable: %v", err)
		status := http.StatusInternalServerError
		body := gin.H{"error": "Failed to preview timetable", "details": err.Error()}
		if result != nil {
			body["status"] = result.Status
			body["unsat_reasons"] = result.UnsatReasons
			body["engine"] = result.Engine
			body["required_sessions"] = result.RequiredSessions
			body["scheduled_sessions"] = result.ScheduledSessions
		}
		if errors.Is(err, services.ErrInfeasible) || strings.Contains(err.Error(), "infeasible") {
			status = http.StatusUnprocessableEntity
		}
		ctx.JSON(status, body)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message":                   "Timetable preview generated (not persisted; commit via POST /generate)",
		"timetables":                result.Timetables,
		"count":                     len(result.Timetables),
		"status":                    result.Status,
		"violated_soft_constraints": result.ViolatedSoftConstraints,
		"engine":                    result.Engine,
		"required_sessions":         result.RequiredSessions,
		"scheduled_sessions":        result.ScheduledSessions,
	})
}

func (c *TimetableController) ValidateTimetable(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{"message": "Use POST create/update for slot validation; conflicts return 409"})
}
