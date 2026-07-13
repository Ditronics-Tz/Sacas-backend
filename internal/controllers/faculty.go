package controllers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

type FacultyController struct {
	facultyRepo repositories.FacultyRepository
}

func NewFacultyController(facultyRepo repositories.FacultyRepository) *FacultyController {
	return &FacultyController{
		facultyRepo: facultyRepo,
	}
}

type CreateFacultyRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=100"`
	Description string `json:"description,omitempty"`
	HodName     string `json:"hod_name,omitempty"`
	HodPhone    string `json:"hod_phone,omitempty"`
	HodEmail    string `json:"hod_email,omitempty"`
}

type UpdateFacultyRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=2,max=100"`
	Description *string `json:"description,omitempty"`
	HodName     *string `json:"hod_name,omitempty"`
	HodPhone    *string `json:"hod_phone,omitempty"`
	HodEmail    *string `json:"hod_email,omitempty"`
}

func (c *FacultyController) CreateFaculty(ctx *gin.Context) {
	var req CreateFacultyRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	faculty := &models.Faculty{
		Name:        req.Name,
		Description: req.Description,
		HodName:     req.HodName,
		HodPhone:    req.HodPhone,
		HodEmail:    req.HodEmail,
	}

	if err := c.facultyRepo.Create(faculty); err != nil {
		logger.Error("Failed to create faculty: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create faculty"})
		return
	}

	ctx.JSON(http.StatusCreated, gin.H{"message": "Faculty created successfully", "faculty": faculty})
}

func (c *FacultyController) GetFaculty(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid faculty ID"})
		return
	}

	faculty, err := c.facultyRepo.GetByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Faculty not found"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"faculty": faculty})
}

func (c *FacultyController) GetAllFaculties(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	faculties, err := c.facultyRepo.GetAll(limit, offset)
	if err != nil {
		logger.Error("Failed to get faculties: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get faculties"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"faculties": faculties, "limit": limit, "offset": offset})
}

func (c *FacultyController) UpdateFaculty(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid faculty ID"})
		return
	}

	var req UpdateFacultyRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	faculty, err := c.facultyRepo.GetByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Faculty not found"})
		return
	}

	if req.Name != nil {
		faculty.Name = *req.Name
	}
	if req.Description != nil {
		faculty.Description = *req.Description
	}
	if req.HodName != nil {
		faculty.HodName = *req.HodName
	}
	if req.HodPhone != nil {
		faculty.HodPhone = *req.HodPhone
	}
	if req.HodEmail != nil {
		faculty.HodEmail = *req.HodEmail
	}

	if err := c.facultyRepo.Update(faculty); err != nil {
		logger.Error("Failed to update faculty: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update faculty"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Faculty updated successfully", "faculty": faculty})
}

func (c *FacultyController) DeleteFaculty(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid faculty ID"})
		return
	}

	if err := c.facultyRepo.Delete(uint(id)); err != nil {
		logger.Error("Failed to delete faculty: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete faculty"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Faculty deleted successfully"})
}
