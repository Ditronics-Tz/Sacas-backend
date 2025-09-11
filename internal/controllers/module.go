package controllers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

type ModuleController struct {
	moduleRepo repositories.ModuleRepository
}

func NewModuleController(moduleRepo repositories.ModuleRepository) *ModuleController {
	return &ModuleController{moduleRepo: moduleRepo}
}

type CreateModuleRequest struct {
	Name        string             `json:"name" binding:"required,min=2,max=100"`
	CourseID    *uint              `json:"course_id"`
	CreditHours int                `json:"credit_hours" binding:"required,min=1,max=10"`
	Type        models.ModuleType  `json:"type" binding:"required"`
	RequiresLab bool               `json:"requires_lab"`
}

func (c *ModuleController) CreateModule(ctx *gin.Context) {
	var req CreateModuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	module := &models.Module{
		Name:        req.Name,
		CourseID:    req.CourseID,
		CreditHours: req.CreditHours,
		Type:        req.Type,
		RequiresLab: req.RequiresLab,
	}

	if err := c.moduleRepo.Create(module); err != nil {
		logger.Error("Failed to create module: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create module"})
		return
	}

	created, _ := c.moduleRepo.GetByID(module.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Module created successfully", "module": created})
}

func (c *ModuleController) GetModule(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	module, err := c.moduleRepo.GetByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Module not found"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"module": module})
}

func (c *ModuleController) GetAllModules(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))

	modules, err := c.moduleRepo.GetAll(limit, offset)
	if err != nil {
		logger.Error("Failed to get modules: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get modules"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"modules": modules, "limit": limit, "offset": offset})
}

func (c *ModuleController) DeleteModule(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	if err := c.moduleRepo.Delete(uint(id)); err != nil {
		logger.Error("Failed to delete module: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete module"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Module deleted successfully"})
}