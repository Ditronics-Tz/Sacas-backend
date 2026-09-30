package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

type SubjectController struct {
	subjectRepo repositories.SubjectRepository
}

func NewSubjectController(subjectRepo repositories.SubjectRepository) *SubjectController {
	return &SubjectController{subjectRepo: subjectRepo}
}

type CreateSubjectRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=100"`
	CreditHours int    `json:"credit_hours" binding:"required,min=1,max=10"`
}

type UpdateSubjectRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=2,max=100"`
	CreditHours *int    `json:"credit_hours,omitempty" binding:"omitempty,min=1,max=10"`
}

func (c *SubjectController) CreateSubject(ctx *gin.Context) {
	inst := tenantID(ctx)

	var req CreateSubjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	subject := &models.Subject{
		Name:        req.Name,
		CreditHours: req.CreditHours,
		// Stamped from the session, never from the payload. General subjects
		// are per-institution catalogue entries, not shared platform rows.
		InstitutionID: inst,
	}

	if err := c.subjectRepo.Create(inst, subject); err != nil {
		logger.Error("Failed to create subject: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create subject"})
		return
	}

	ctx.JSON(http.StatusCreated, gin.H{"message": "Subject created successfully", "subject": subject})
}

func (c *SubjectController) GetSubject(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid subject ID"})
		return
	}

	subject, err := c.subjectRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Subject not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"subject": subject})
}

func (c *SubjectController) GetAllSubjects(ctx *gin.Context) {
	inst := tenantID(ctx)

	limit, offset := parsePagination(ctx)

	subjects, err := c.subjectRepo.GetAll(inst, limit, offset)
	if err != nil {
		logger.Error("Failed to get subjects: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subjects"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"subjects": subjects, "limit": limit, "offset": offset})
}

func (c *SubjectController) UpdateSubject(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid subject ID"})
		return
	}

	var req UpdateSubjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	subject, err := c.subjectRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Subject not found", err)
		return
	}

	if req.Name != nil {
		subject.Name = *req.Name
	}
	if req.CreditHours != nil {
		subject.CreditHours = *req.CreditHours
	}

	if err := c.subjectRepo.Update(inst, subject); err != nil {
		logger.Error("Failed to update subject: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update subject"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Subject updated successfully", "subject": subject})
}

func (c *SubjectController) DeleteSubject(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid subject ID"})
		return
	}

	if err := c.subjectRepo.Delete(inst, id); err != nil {
		logger.Error("Failed to delete subject: %v", err)
		respondRepoError(ctx, "Subject not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Subject deleted successfully"})
}
