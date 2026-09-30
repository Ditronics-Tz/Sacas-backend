package controllers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
)

type TimetableController struct {
	timetableRepo    repositories.TimetableRepository
	staffRepo        repositories.StaffRepository
	classRepo        repositories.ClassRepository
	roomRepo         repositories.RoomRepository
	timetableService *services.TimetableService
}

func NewTimetableController(
	timetableRepo repositories.TimetableRepository,
	staffRepo repositories.StaffRepository,
	classRepo repositories.ClassRepository,
	roomRepo repositories.RoomRepository,
	timetableService *services.TimetableService,
) *TimetableController {
	return &TimetableController{
		timetableRepo:    timetableRepo,
		staffRepo:        staffRepo,
		classRepo:        classRepo,
		roomRepo:         roomRepo,
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

// classInInstitution confirms the class belongs to the caller's tenant. It is
// the gate that stops a create or generate request from writing timetable rows
// into another institution.
func (c *TimetableController) classInInstitution(institutionID, classID uint) error {
	if c.classRepo == nil {
		return errors.New("class repository unavailable")
	}
	_, err := c.classRepo.GetByID(institutionID, classID)
	return err
}

func (c *TimetableController) CreateTimetable(ctx *gin.Context) {
	inst := tenantID(ctx)

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

	// The class, staff, and room must all belong to the caller's institution.
	// Otherwise the entry would reference (and preload) another tenant's rows.
	if err := c.classInInstitution(inst, req.ClassID); err != nil {
		respondNotFound(ctx, "Class not found")
		return
	}
	if _, err := c.staffRepo.GetByID(inst, req.StaffID); err != nil {
		respondNotFound(ctx, "Staff not found")
		return
	}
	if c.roomRepo != nil {
		if _, err := c.roomRepo.GetByID(inst, req.RoomID); err != nil {
			respondNotFound(ctx, "Room not found")
			return
		}
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
		// Stamped from the session, never from the payload.
		InstitutionID: inst,
	}

	if err := c.timetableService.ValidateTimeSlot(inst, timetable, 0); err != nil {
		logger.Error("Timetable validation failed: %v", err)
		ctx.JSON(http.StatusConflict, gin.H{"error": "Scheduling conflict detected", "details": err.Error()})
		return
	}

	if err := c.timetableRepo.Create(inst, timetable); err != nil {
		logger.Error("Failed to create timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create timetable"})
		return
	}

	created, err := c.timetableRepo.GetByID(inst, timetable.ID)
	if err != nil {
		logger.Error("Failed to fetch created timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch created timetable"})
		return
	}

	logger.Info("Timetable entry created successfully: ID %d", created.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Timetable entry created successfully", "timetable": created})
}

func (c *TimetableController) GetTimetable(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timetable ID"})
		return
	}

	timetable, err := c.timetableRepo.GetByID(inst, id)
	if err != nil {
		logger.Error("Failed to get timetable: %v", err)
		respondRepoError(ctx, "Timetable entry not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"timetable": timetable})
}

func (c *TimetableController) GetTimetableByClass(ctx *gin.Context) {
	inst := tenantID(ctx)

	classID, err := parseIDParam(ctx, "class_id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid class ID"})
		return
	}

	// A class from another institution is reported as not found rather than
	// as an empty timetable.
	if err := c.classInInstitution(inst, classID); err != nil {
		respondNotFound(ctx, "Class not found")
		return
	}

	timetables, err := c.timetableRepo.GetByClass(inst, classID)
	if err != nil {
		logger.Error("Failed to get timetable for class: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get timetable"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"timetables": timetables})
}

func (c *TimetableController) GetTimetableByStaff(ctx *gin.Context) {
	inst := tenantID(ctx)

	// Route is /by-staff/:staff_id to avoid clashing with staff CRUD /staff/:id
	staffID, err := parseIDParam(ctx, "staff_id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid staff ID"})
		return
	}

	if _, err := c.staffRepo.GetByID(inst, staffID); err != nil {
		respondNotFound(ctx, "Staff not found")
		return
	}

	timetables, err := c.timetableRepo.GetByStaff(inst, staffID)
	if err != nil {
		logger.Error("Failed to get timetable for staff: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get timetable"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"timetables": timetables})
}

// GetTimetableByCourse handles GET /by-course/:course_id — aggregates
// timetable entries for all classes belonging to a course.
// A course with no classes (or no entries) returns 200 with an empty array,
// not 404 — that is a legitimate state, not an error.
func (c *TimetableController) GetTimetableByCourse(ctx *gin.Context) {
	inst := tenantID(ctx)

	courseID, err := parseIDParam(ctx, "course_id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course ID"})
		return
	}

	timetables, err := c.timetableRepo.GetByCourse(inst, courseID)
	if err != nil {
		logger.Error("Failed to get timetable for course: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get timetable"})
		return
	}
	if timetables == nil {
		timetables = []models.Timetable{}
	}

	ctx.JSON(http.StatusOK, gin.H{"timetables": timetables})
}

// GetMyTimetable handles GET /protected/timetable/my.
//
// Security invariants:
//   - Requires authentication (JWT middleware on the protected group).
//   - The Staff record is resolved ONLY from the authenticated user_id
//     (Staff.user_id FK), and only within the caller's institution. Any
//     staff_id/query/body value sent by the client is ignored, so a role=user
//     account can never read another staff member's timetable — including one
//     at a different institution — by manipulating an ID.
//   - Existing admin endpoints (/timetable/by-staff/:staff_id etc.) are
//     untouched.
func (c *TimetableController) GetMyTimetable(ctx *gin.Context) {
	inst := tenantID(ctx)

	userID, ok := currentUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	// Resolve that User to their Staff record via the existing FK
	// relationship (staff.user_id). No client-provided staff_id is read.
	staff, err := c.staffRepo.GetByUserID(inst, userID)
	if err != nil {
		logger.Warn("No staff profile linked to user %d for /timetable/my", userID)
		ctx.JSON(http.StatusNotFound, gin.H{"error": "No staff profile is linked to your account"})
		return
	}

	// Return only this staff member's timetable, in the frontend-compatible shape.
	timetables, err := c.timetableRepo.GetByStaff(inst, staff.ID)
	if err != nil {
		logger.Error("Failed to get timetable for staff %d: %v", staff.ID, err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get timetable"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"timetables": timetables})
}

func (c *TimetableController) UpdateTimetable(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
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

	// An entry owned by another institution resolves to not-found here, so the
	// update below can never reach it.
	timetable, err := c.timetableRepo.GetByID(inst, id)
	if err != nil {
		logger.Error("Failed to get timetable: %v", err)
		respondRepoError(ctx, "Timetable entry not found", err)
		return
	}

	if req.ClassID != nil {
		if err := c.classInInstitution(inst, *req.ClassID); err != nil {
			respondNotFound(ctx, "Class not found")
			return
		}
		timetable.ClassID = *req.ClassID
	}
	if req.ModuleID != nil {
		timetable.ModuleID = req.ModuleID
	}
	if req.SubjectID != nil {
		timetable.SubjectID = req.SubjectID
	}
	if req.StaffID != nil {
		if _, err := c.staffRepo.GetByID(inst, *req.StaffID); err != nil {
			respondNotFound(ctx, "Staff not found")
			return
		}
		timetable.StaffID = *req.StaffID
	}
	if req.RoomID != nil {
		if c.roomRepo != nil {
			if _, err := c.roomRepo.GetByID(inst, *req.RoomID); err != nil {
				respondNotFound(ctx, "Room not found")
				return
			}
		}
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

	if err := c.timetableService.ValidateTimeSlot(inst, timetable, id); err != nil {
		logger.Error("Timetable validation failed: %v", err)
		ctx.JSON(http.StatusConflict, gin.H{"error": "Scheduling conflict detected", "details": err.Error()})
		return
	}

	if err := c.timetableRepo.Update(inst, timetable); err != nil {
		logger.Error("Failed to update timetable: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update timetable"})
		return
	}

	logger.Info("Timetable entry updated successfully: ID %d", timetable.ID)
	ctx.JSON(http.StatusOK, gin.H{"message": "Timetable entry updated successfully", "timetable": timetable})
}

func (c *TimetableController) DeleteTimetable(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timetable ID"})
		return
	}

	if err := c.timetableRepo.Delete(inst, id); err != nil {
		logger.Error("Failed to delete timetable: %v", err)
		respondRepoError(ctx, "Timetable entry not found", err)
		return
	}

	logger.Info("Timetable entry deleted successfully: ID %d", id)
	ctx.JSON(http.StatusOK, gin.H{"message": "Timetable entry deleted successfully"})
}

func (c *TimetableController) GenerateTimetable(ctx *gin.Context) {
	inst := tenantID(ctx)

	var req GenerateTimetableRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid request payload: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	result, err := c.timetableService.GenerateTimetable(inst, req.ClassID)
	if err != nil {
		logger.Error("Failed to generate timetable: %v", err)
		status := http.StatusInternalServerError
		body := gin.H{"error": "Failed to generate timetable", "details": err.Error()}
		// A class from another institution is a 404, not a generation failure.
		if errors.Is(err, services.ErrClassNotInInstitution) {
			respondNotFound(ctx, "Class not found")
			return
		}
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
		if errors.Is(err, services.ErrInfeasible) || strings.Contains(err.Error(), "infeasible") {
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
	inst := tenantID(ctx)

	var req GenerateTimetableRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	result, err := c.timetableService.PreviewTimetable(inst, req.ClassID)
	if err != nil {
		logger.Error("Failed to preview timetable: %v", err)
		status := http.StatusInternalServerError
		body := gin.H{"error": "Failed to preview timetable", "details": err.Error()}
		if errors.Is(err, services.ErrClassNotInInstitution) {
			respondNotFound(ctx, "Class not found")
			return
		}
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
