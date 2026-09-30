package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

type CourseController struct {
	courseRepo repositories.CourseRepository
}

func NewCourseController(courseRepo repositories.CourseRepository) *CourseController {
	return &CourseController{courseRepo: courseRepo}
}

type CreateCourseRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=100"`
	FacultyID   uint   `json:"faculty_id" binding:"required"`
	Description string `json:"description,omitempty"`
	Level       string `json:"level,omitempty"`
}

type UpdateCourseRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=2,max=100"`
	FacultyID   *uint   `json:"faculty_id,omitempty"`
	Description *string `json:"description,omitempty"`
	Level       *string `json:"level,omitempty"`
}

func (c *CourseController) CreateCourse(ctx *gin.Context) {
	inst := tenantID(ctx)

	var req CreateCourseRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	course := &models.Course{
		Name:        req.Name,
		FacultyID:   req.FacultyID,
		Description: req.Description,
		Level:       req.Level,
		// Stamped from the session, never from the payload.
		InstitutionID: inst,
	}

	if err := c.courseRepo.Create(inst, course); err != nil {
		logger.Error("Failed to create course: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create course"})
		return
	}

	created, _ := c.courseRepo.GetByID(inst, course.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Course created successfully", "course": created})
}

func (c *CourseController) GetCourse(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course ID"})
		return
	}

	course, err := c.courseRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Course not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"course": course})
}

func (c *CourseController) GetAllCourses(ctx *gin.Context) {
	inst := tenantID(ctx)

	limit, offset := parsePagination(ctx)

	courses, err := c.courseRepo.GetAll(inst, limit, offset)
	if err != nil {
		logger.Error("Failed to get courses: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get courses"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"courses": courses, "limit": limit, "offset": offset})
}

func (c *CourseController) UpdateCourse(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course ID"})
		return
	}

	var req UpdateCourseRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	course, err := c.courseRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Course not found", err)
		return
	}

	if req.Name != nil {
		course.Name = *req.Name
	}
	if req.FacultyID != nil {
		course.FacultyID = *req.FacultyID
	}
	if req.Description != nil {
		course.Description = *req.Description
	}
	if req.Level != nil {
		course.Level = *req.Level
	}

	if err := c.courseRepo.Update(inst, course); err != nil {
		logger.Error("Failed to update course: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update course"})
		return
	}

	updated, _ := c.courseRepo.GetByID(inst, course.ID)
	ctx.JSON(http.StatusOK, gin.H{"message": "Course updated successfully", "course": updated})
}

func (c *CourseController) DeleteCourse(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course ID"})
		return
	}

	if err := c.courseRepo.Delete(inst, id); err != nil {
		logger.Error("Failed to delete course: %v", err)
		respondRepoError(ctx, "Course not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Course deleted successfully"})
}
