package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

type ModuleController struct {
	moduleRepo repositories.ModuleRepository
	// courseRepo is used to verify that a referenced course belongs to the
	// caller's institution. Without it, a module could point at another
	// tenant's course and leak that course's name through the preload.
	courseRepo repositories.CourseRepository
}

func NewModuleController(moduleRepo repositories.ModuleRepository, courseRepo repositories.CourseRepository) *ModuleController {
	return &ModuleController{moduleRepo: moduleRepo, courseRepo: courseRepo}
}

// courseExists confirms a course ID resolves inside the caller's institution.
func (c *ModuleController) courseExists(_ *gin.Context, institutionID, courseID uint) (*models.Course, error) {
	if c.courseRepo == nil {
		return nil, errors.New("course repository unavailable")
	}
	return c.courseRepo.GetByID(institutionID, courseID)
}

type CreateModuleRequest struct {
	Name        string            `json:"name" binding:"required,min=2,max=100"`
	Code        string            `json:"code,omitempty"`
	CourseID    *uint             `json:"course_id"`
	CreditHours int               `json:"credit_hours" binding:"required,min=1,max=10"`
	Type        models.ModuleType `json:"type" binding:"required"`
	RequiresLab bool              `json:"requires_lab"`
	Semester    *int              `json:"semester,omitempty"`
	NtaLevel    string            `json:"nta_level,omitempty"`
}

type UpdateModuleRequest struct {
	Name        *string            `json:"name,omitempty" binding:"omitempty,min=2,max=100"`
	Code        *string            `json:"code,omitempty"`
	CourseID    *uint              `json:"course_id"`
	CreditHours *int               `json:"credit_hours,omitempty" binding:"omitempty,min=1,max=10"`
	Type        *models.ModuleType `json:"type,omitempty"`
	RequiresLab *bool              `json:"requires_lab,omitempty"`
	Semester    *int               `json:"semester,omitempty"`
	NtaLevel    *string            `json:"nta_level,omitempty"`
	ClearCourse bool               `json:"clear_course,omitempty"`
}

func (c *ModuleController) CreateModule(ctx *gin.Context) {
	inst := tenantID(ctx)

	var req CreateModuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	if !req.Type.IsValid() {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module type", "details": "must be core, elective, or general_subject"})
		return
	}

	if req.Type == models.ModuleTypeGeneral {
		req.CourseID = nil
	} else if req.CourseID == nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "course_id is required for core and elective modules"})
		return
	}

	// A module may only hang off a course in the same institution. Without
	// this check a tenant could reference another tenant's course ID and pull
	// its modules into this tenant's generation run.
	if req.CourseID != nil {
		course, err := c.courseExists(ctx, inst, *req.CourseID)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course_id", "details": "course not found in this institution"})
			return
		}
		_ = course
	}

	module := &models.Module{
		Name:        req.Name,
		Code:        req.Code,
		CourseID:    req.CourseID,
		CreditHours: req.CreditHours,
		Type:        req.Type,
		RequiresLab: req.RequiresLab,
		Semester:    req.Semester,
		NtaLevel:    req.NtaLevel,
		// Stamped from the session, never from the payload.
		InstitutionID: inst,
	}

	if err := c.moduleRepo.Create(inst, module); err != nil {
		logger.Error("Failed to create module: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create module"})
		return
	}

	created, _ := c.moduleRepo.GetByID(inst, module.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Module created successfully", "module": created})
}

func (c *ModuleController) GetModule(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	module, err := c.moduleRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Module not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"module": module})
}

func (c *ModuleController) GetAllModules(ctx *gin.Context) {
	inst := tenantID(ctx)

	limit, offset := parsePagination(ctx)

	modules, err := c.moduleRepo.GetAll(inst, limit, offset)
	if err != nil {
		logger.Error("Failed to get modules: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get modules"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"modules": modules, "limit": limit, "offset": offset})
}

func (c *ModuleController) UpdateModule(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	var req UpdateModuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	module, err := c.moduleRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Module not found", err)
		return
	}

	if req.Name != nil {
		module.Name = *req.Name
	}
	if req.Code != nil {
		module.Code = *req.Code
	}
	if req.ClearCourse {
		module.CourseID = nil
	} else if req.CourseID != nil {
		// Re-pointing a module at another institution's course is rejected.
		if _, err := c.courseExists(ctx, inst, *req.CourseID); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid course_id", "details": "course not found in this institution"})
			return
		}
		module.CourseID = req.CourseID
	}
	if req.CreditHours != nil {
		module.CreditHours = *req.CreditHours
	}
	if req.Type != nil {
		if !req.Type.IsValid() {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module type"})
			return
		}
		module.Type = *req.Type
		if *req.Type == models.ModuleTypeGeneral {
			module.CourseID = nil
		}
	}
	if req.RequiresLab != nil {
		module.RequiresLab = *req.RequiresLab
	}
	if req.Semester != nil {
		module.Semester = req.Semester
	}
	if req.NtaLevel != nil {
		module.NtaLevel = *req.NtaLevel
	}

	if err := c.moduleRepo.Update(inst, module); err != nil {
		logger.Error("Failed to update module: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update module"})
		return
	}

	updated, _ := c.moduleRepo.GetByID(inst, module.ID)
	ctx.JSON(http.StatusOK, gin.H{"message": "Module updated successfully", "module": updated})
}

func (c *ModuleController) DeleteModule(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module ID"})
		return
	}

	if err := c.moduleRepo.Delete(inst, id); err != nil {
		logger.Error("Failed to delete module: %v", err)
		respondRepoError(ctx, "Module not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Module deleted successfully"})
}
