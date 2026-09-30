package controllers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
)

// ExamController serves exam scheduling for one institution.
//
// Every referenced record (course, module, room, invigilator) must belong to the
// caller's own institution. That is enforced here rather than left to the
// database, because a cross-tenant room or lecturer reference would both leak
// another campus's data in the response and let an exam be scheduled somewhere
// impossible.
type ExamController struct {
	examRepo   repositories.ExamRepository
	courseRepo repositories.CourseRepository
	moduleRepo repositories.ModuleRepository
	classRepo  repositories.ClassRepository
	roomRepo   repositories.RoomRepository
	staffRepo  repositories.StaffRepository
	audit      *services.AuditRecorder
}

func NewExamController(
	examRepo repositories.ExamRepository,
	courseRepo repositories.CourseRepository,
	moduleRepo repositories.ModuleRepository,
	classRepo repositories.ClassRepository,
	roomRepo repositories.RoomRepository,
	staffRepo repositories.StaffRepository,
	audit *services.AuditRecorder,
) *ExamController {
	return &ExamController{
		examRepo: examRepo, courseRepo: courseRepo, moduleRepo: moduleRepo,
		classRepo: classRepo, roomRepo: roomRepo, staffRepo: staffRepo,
		audit: audit,
	}
}

type CreateExamRequest struct {
	Title string `json:"title" binding:"required,min=2,max=150"`
	Type  string `json:"type" binding:"required"`
	// CourseID is required; ModuleID and the class set are optional.
	CourseID uint   `json:"course_id" binding:"required"`
	ModuleID *uint  `json:"module_id,omitempty"`
	ClassIDs []uint `json:"class_ids,omitempty"`

	ExamDate  string `json:"exam_date" binding:"required"`
	StartTime string `json:"start_time" binding:"required"`
	EndTime   string `json:"end_time" binding:"required"`

	RoomID        *uint  `json:"room_id,omitempty"`
	InvigilatorID *uint  `json:"invigilator_id,omitempty"`
	MaxMarks      int    `json:"max_marks,omitempty"`
	Notes         string `json:"notes,omitempty"`
}

type UpdateExamRequest struct {
	Title         *string `json:"title,omitempty" binding:"omitempty,min=2,max=150"`
	Type          *string `json:"type,omitempty"`
	CourseID      *uint   `json:"course_id,omitempty"`
	ModuleID      *uint   `json:"module_id,omitempty"`
	ClassIDs      *[]uint `json:"class_ids,omitempty"`
	ExamDate      *string `json:"exam_date,omitempty"`
	StartTime     *string `json:"start_time,omitempty"`
	EndTime       *string `json:"end_time,omitempty"`
	RoomID        *uint   `json:"room_id,omitempty"`
	InvigilatorID *uint   `json:"invigilator_id,omitempty"`
	MaxMarks      *int    `json:"max_marks,omitempty"`
	Notes         *string `json:"notes,omitempty"`
	// ClearRoom and ClearInvigilator un-set the scheduling references, returning
	// the exam to an unscheduled draft.
	ClearRoom        bool `json:"clear_room,omitempty"`
	ClearInvigilator bool `json:"clear_invigilator,omitempty"`
}

func (c *ExamController) Create(ctx *gin.Context) {
	inst := tenantID(ctx)

	var req CreateExamRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	examType := models.ExamType(req.Type)
	if !examType.IsValid() {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid exam type",
			"details": "must be one of: midterm, final, quiz, practical, supplement",
		})
		return
	}
	if req.StartTime >= req.EndTime {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "end_time must be after start_time"})
		return
	}
	if !isISODate(req.ExamDate) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "exam_date must be YYYY-MM-DD"})
		return
	}
	// Every reference is verified inside the institution before anything is
	// written.
	if !c.validateReferences(ctx, inst, req.CourseID, req.ModuleID, req.ClassIDs, req.RoomID, req.InvigilatorID) {
		return
	}

	maxMarks := req.MaxMarks
	if maxMarks == 0 {
		maxMarks = 100
	}

	exam := &models.Exam{
		Title:         req.Title,
		Type:          examType,
		CourseID:      req.CourseID,
		ModuleID:      req.ModuleID,
		ClassIDs:      req.ClassIDs,
		ExamDate:      req.ExamDate,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
		RoomID:        req.RoomID,
		InvigilatorID: req.InvigilatorID,
		MaxMarks:      maxMarks,
		Notes:         req.Notes,
		Status:        models.ExamStatusDraft,
		// Stamped from the session, never the payload.
		InstitutionID: inst,
	}

	// A room or invigilator clash is checked at creation as well as on schedule,
	// because an exam can be created already carrying both.
	if err := c.checkConflicts(ctx, inst, exam, 0); err != nil {
		return
	}

	if err := c.examRepo.Create(inst, exam); err != nil {
		logger.Error("Failed to create exam: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create exam"})
		return
	}

	c.audit.Record(ctx, models.AuditExamCreate, "exam", uintString(exam.ID), exam.Title,
		map[string]any{"course_id": exam.CourseID, "date": exam.ExamDate, "type": string(exam.Type)})

	ctx.JSON(http.StatusCreated, gin.H{"message": "Exam created", "exam": exam})
}

func (c *ExamController) Get(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid exam ID"})
		return
	}
	exam, err := c.examRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Exam not found", err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"exam": exam})
}

func (c *ExamController) GetAll(ctx *gin.Context) {
	inst := tenantID(ctx)
	limit, offset := parsePagination(ctx)

	examRepo := c.examRepo
	var (
		exams []models.Exam
		err   error
	)
	switch {
	case ctx.Query("course_id") != "":
		courseID, perr := parseIDParam(ctx, "course_id")
		if perr != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course_id"})
			return
		}
		exams, err = examRepo.GetByCourse(inst, courseID, limit, offset)
	case ctx.Query("status") != "":
		status := models.ExamStatus(ctx.Query("status"))
		if !status.IsValid() {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid status",
				"details": "must be one of: draft, scheduled, published, approved",
			})
			return
		}
		exams, err = examRepo.GetByStatus(inst, status, limit, offset)
	default:
		exams, err = examRepo.GetAll(inst, limit, offset)
	}
	if err != nil {
		logger.Error("Failed to list exams: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list exams"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"exams": exams, "limit": limit, "offset": offset})
}

func (c *ExamController) Update(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid exam ID"})
		return
	}
	exam, err := c.examRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Exam not found", err)
		return
	}

	var req UpdateExamRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}
	// An approved exam is signed off; editing it afterwards would change a
	// document somebody has already approved.
	if exam.Status == models.ExamStatusApproved {
		ctx.JSON(http.StatusConflict, gin.H{
			"error":   "An approved exam cannot be edited",
			"details": "Create a supplementary exam instead.",
		})
		return
	}

	if req.Title != nil {
		exam.Title = *req.Title
	}
	if req.Type != nil {
		t := models.ExamType(*req.Type)
		if !t.IsValid() {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid exam type"})
			return
		}
		exam.Type = t
	}
	if req.CourseID != nil {
		exam.CourseID = *req.CourseID
	}
	if req.ModuleID != nil {
		exam.ModuleID = req.ModuleID
	}
	if req.ClassIDs != nil {
		exam.ClassIDs = *req.ClassIDs
	}
	if req.ExamDate != nil {
		exam.ExamDate = *req.ExamDate
	}
	if req.StartTime != nil {
		exam.StartTime = *req.StartTime
	}
	if req.EndTime != nil {
		exam.EndTime = *req.EndTime
	}
	if req.MaxMarks != nil {
		exam.MaxMarks = *req.MaxMarks
	}
	if req.Notes != nil {
		exam.Notes = *req.Notes
	}
	switch {
	case req.ClearRoom:
		exam.RoomID = nil
	case req.RoomID != nil:
		exam.RoomID = req.RoomID
	}
	switch {
	case req.ClearInvigilator:
		exam.InvigilatorID = nil
	case req.InvigilatorID != nil:
		exam.InvigilatorID = req.InvigilatorID
	}

	if exam.StartTime >= exam.EndTime {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "end_time must be after start_time"})
		return
	}
	if !c.validateReferences(ctx, inst, exam.CourseID, exam.ModuleID, exam.ClassIDs, exam.RoomID, exam.InvigilatorID) {
		return
	}
	if err := c.checkConflicts(ctx, inst, exam, exam.ID); err != nil {
		return
	}

	// Editing the logistics of a published exam invalidates the publication, so
	// it drops back to scheduled. A published exam that silently keeps its
	// published state while its room changes is a real-world problem.
	if exam.Status == models.ExamStatusPublished {
		exam.Status = models.ExamStatusScheduled
		exam.PublishedAt = nil
		exam.PublishedByID = nil
	}

	if err := c.examRepo.Update(inst, exam); err != nil {
		logger.Error("Failed to update exam %d: %v", id, err)
		respondRepoError(ctx, "Exam not found", err)
		return
	}

	c.audit.Record(ctx, models.AuditExamUpdate, "exam", uintString(id), exam.Title,
		map[string]any{"course_id": exam.CourseID, "date": exam.ExamDate})

	ctx.JSON(http.StatusOK, gin.H{"message": "Exam updated", "exam": exam})
}

func (c *ExamController) Delete(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid exam ID"})
		return
	}
	exam, err := c.examRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Exam not found", err)
		return
	}
	deletedTitle := exam.Title

	if err := c.examRepo.Delete(inst, id); err != nil {
		logger.Error("Failed to delete exam %d: %v", id, err)
		respondRepoError(ctx, "Exam not found", err)
		return
	}

	c.audit.Record(ctx, models.AuditExamDelete, "exam", uintString(id), deletedTitle,
		map[string]any{"severity": "high"})

	ctx.JSON(http.StatusOK, gin.H{"message": "Exam deleted"})
}

// SetStatus handles POST /exams/:id/status for the schedule, publish, and
// approve transitions.
//
// The three are separate endpoints in the router, each behind its own
// permission, so the shared handler is given the permitted target status.
func (c *ExamController) SetStatus(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid exam ID"})
		return
	}
	exam, err := c.examRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Exam not found", err)
		return
	}

	target := models.ExamStatus(ctx.Param("status"))
	if !target.IsValid() {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status"})
		return
	}
	// Only the three forward transitions the permissions cover are reachable
	// through this route; a caller cannot ask for `draft` and have the
	// permission check for `publish` apply.
	switch target {
	case models.ExamStatusScheduled, models.ExamStatusPublished, models.ExamStatusApproved:
	default:
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Status may only be set to scheduled, published, or approved",
			"details": "Use PATCH to correct the details of an exam.",
		})
		return
	}

	// Publishing and approving require the exam to be actually scheduled. An
	// exam with no room or invigilator is not a real exam, and approving it would
	// let results be entered against a sitting nobody was told about.
	if target == models.ExamStatusPublished || target == models.ExamStatusApproved {
		if !exam.IsScheduled() {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "This exam is not fully scheduled",
				"details": "Set a room and an invigilator before publishing.",
			})
			return
		}
		if err := c.checkConflicts(ctx, inst, exam, exam.ID); err != nil {
			return
		}
	}

	userID, _ := currentUserID(ctx)
	ok, err := c.examRepo.SetStatus(inst, id, target, userID, timeNow())
	if err != nil {
		if err == repositories.ErrInvalidExamTransition {
			ctx.JSON(http.StatusConflict, gin.H{
				"error":   "That status change is not allowed",
				"details": "Statuses move forward only, and an approved exam cannot change.",
				"from":    string(exam.Status),
				"to":      string(target),
			})
			return
		}
		logger.Error("Failed to set exam status %d: %v", id, err)
		respondRepoError(ctx, "Exam not found", err)
		return
	}
	if !ok {
		// The row moved between the read and the write: another request won.
		ctx.JSON(http.StatusConflict, gin.H{
			"error":   "The exam's status changed while you were working",
			"details": "Reload the exam and try again.",
		})
		return
	}

	switch target {
	case models.ExamStatusScheduled:
		c.audit.Record(ctx, models.AuditExamSchedule, "exam", uintString(id), exam.Title,
			map[string]any{"date": exam.ExamDate, "room_id": exam.RoomID, "invigilator_id": exam.InvigilatorID})
	case models.ExamStatusPublished:
		c.audit.Record(ctx, models.AuditExamPublish, "exam", uintString(id), exam.Title,
			map[string]any{"date": exam.ExamDate, "course_id": exam.CourseID})
	case models.ExamStatusApproved:
		c.audit.Record(ctx, models.AuditExamApprove, "exam", uintString(id), exam.Title,
			map[string]any{"date": exam.ExamDate, "course_id": exam.CourseID})
	}

	updated, _ := c.examRepo.GetByID(inst, id)
	ctx.JSON(http.StatusOK, gin.H{"message": "Exam status updated", "exam": updated})
}

// --- helpers -----------------------------------------------------------------

// validateReferences confirms every referenced record lives in the caller's
// institution. Returns false after writing the error response.
func (c *ExamController) validateReferences(
	ctx *gin.Context,
	inst uint,
	courseID uint,
	moduleID *uint,
	classIDs []uint,
	roomID *uint,
	invigilatorID *uint,
) bool {
	if c.courseRepo != nil {
		if _, err := c.courseRepo.GetByID(inst, courseID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "course_id not found in this institution",
				"details": "An exam cannot reference another institution's course.",
			})
			return false
		}
	}
	if moduleID != nil && c.moduleRepo != nil {
		if _, err := c.moduleRepo.GetByID(inst, *moduleID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "module_id not found in this institution"})
			return false
		}
	}
	if c.classRepo != nil {
		for _, id := range classIDs {
			if _, err := c.classRepo.GetByID(inst, id); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{
					"error":   "class_ids contains an id not in this institution",
					"details": "An exam cannot include another institution's class.",
				})
				return false
			}
		}
	}
	if roomID != nil && c.roomRepo != nil {
		if _, err := c.roomRepo.GetByID(inst, *roomID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "room_id not found in this institution"})
			return false
		}
	}
	if invigilatorID != nil && c.staffRepo != nil {
		if _, err := c.staffRepo.GetByID(inst, *invigilatorID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "invigilator_id not found in this institution",
				"details": "An invigilator must be staff at the same institution.",
			})
			return false
		}
	}
	return true
}

// checkConflicts reports a room or invigilator clash. Returns a non-nil error
// after writing the response when there is a clash, so callers can simply
// `if err != nil { return }`.
func (c *ExamController) checkConflicts(ctx *gin.Context, inst uint, exam *models.Exam, excludeID uint) error {
	if exam.RoomID != nil {
		conflicts, err := c.examRepo.CheckRoomConflicts(
			inst, *exam.RoomID, exam.ExamDate, exam.StartTime, exam.EndTime, excludeID)
		if err != nil {
			logger.Error("Failed to check room conflicts: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate the room booking"})
			return err
		}
		if len(conflicts) > 0 {
			ctx.JSON(http.StatusConflict, gin.H{
				"error":     "The room is already booked in that slot",
				"conflicts": examSummaries(conflicts),
			})
			return errExamConflict
		}
	}
	if exam.InvigilatorID != nil {
		conflicts, err := c.examRepo.CheckInvigilatorConflicts(
			inst, *exam.InvigilatorID, exam.ExamDate, exam.StartTime, exam.EndTime, excludeID)
		if err != nil {
			logger.Error("Failed to check invigilator conflicts: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate the invigilator"})
			return err
		}
		if len(conflicts) > 0 {
			ctx.JSON(http.StatusConflict, gin.H{
				"error":     "The invigilator is already allocated in that slot",
				"conflicts": examSummaries(conflicts),
			})
			return errExamConflict
		}
	}
	return nil
}

func examSummaries(exams []models.Exam) []gin.H {
	out := make([]gin.H, 0, len(exams))
	for i := range exams {
		out = append(out, gin.H{
			"id":     exams[i].ID,
			"title":  exams[i].Title,
			"date":   exams[i].ExamDate,
			"window": exams[i].StartTime + "-" + exams[i].EndTime,
		})
	}
	return out
}

// isISODate reports whether a string is a YYYY-MM-DD date that really exists.
// Rejecting 2026-02-30 matters: a bad date here silently produces an exam that
// never happens.
func isISODate(value string) bool {
	if len(value) != 10 {
		return false
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return false
	}
	return parsed.Format("2006-01-02") == value
}

// errExamConflict is a sentinel so checkConflicts can tell the caller it already
// wrote a 409.
var errExamConflict = errors.New("exam scheduling conflict")
