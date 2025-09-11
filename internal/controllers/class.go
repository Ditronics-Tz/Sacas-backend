package controllers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

type ClassController struct {
	classRepo repositories.ClassRepository
}

func NewClassController(classRepo repositories.ClassRepository) *ClassController {
	return &ClassController{classRepo: classRepo}
}

type CreateClassRequest struct {
	Name             string `json:"name" binding:"required,min=1,max=100"`
	CourseID         uint   `json:"course_id" binding:"required"`
	Year             int    `json:"year" binding:"required,min=1,max=6"`
	NumberOfStudents int    `json:"number_of_students" binding:"required,min=1"`
}

func (c *ClassController) CreateClass(ctx *gin.Context) {
	var req CreateClassRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	class := &models.Class{
		Name:             req.Name,
		CourseID:         req.CourseID,
		Year:             req.Year,
		NumberOfStudents: req.NumberOfStudents,
	}

	if err := c.classRepo.Create(class); err != nil {
		logger.Error("Failed to create class: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create class"})
		return
	}

	created, _ := c.classRepo.GetByID(class.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Class created successfully", "class": created})
}

func (c *ClassController) GetClass(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid class ID"})
		return
	}

	class, err := c.classRepo.GetByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Class not found"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"class": class})
}

func (c *ClassController) GetAllClasses(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))

	classes, err := c.classRepo.GetAll(limit, offset)
	if err != nil {
		logger.Error("Failed to get classes: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get classes"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"classes": classes, "limit": limit, "offset": offset})
}

func (c *ClassController) DeleteClass(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid class ID"})
		return
	}

	if err := c.classRepo.Delete(uint(id)); err != nil {
		logger.Error("Failed to delete class: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete class"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Class deleted successfully"})
}