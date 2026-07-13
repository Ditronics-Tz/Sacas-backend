package controllers

import (
	"net/http"
	"strconv"

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
	}

	if err := c.courseRepo.Create(course); err != nil {
		logger.Error("Failed to create course: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create course"})
		return
	}

	created, _ := c.courseRepo.GetByID(course.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Course created successfully", "course": created})
}

func (c *CourseController) GetCourse(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course ID"})
		return
	}

	course, err := c.courseRepo.GetByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Course not found"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"course": course})
}

func (c *CourseController) GetAllCourses(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	if limit <= 0 {
		limit = 10
	}

	courses, err := c.courseRepo.GetAll(limit, offset)
	if err != nil {
		logger.Error("Failed to get courses: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get courses"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"courses": courses, "limit": limit, "offset": offset})
}

func (c *CourseController) UpdateCourse(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course ID"})
		return
	}

	var req UpdateCourseRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	course, err := c.courseRepo.GetByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Course not found"})
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

	if err := c.courseRepo.Update(course); err != nil {
		logger.Error("Failed to update course: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update course"})
		return
	}

	updated, _ := c.courseRepo.GetByID(course.ID)
	ctx.JSON(http.StatusOK, gin.H{"message": "Course updated successfully", "course": updated})
}

func (c *CourseController) DeleteCourse(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course ID"})
		return
	}

	if err := c.courseRepo.Delete(uint(id)); err != nil {
		logger.Error("Failed to delete course: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete course"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Course deleted successfully"})
}
