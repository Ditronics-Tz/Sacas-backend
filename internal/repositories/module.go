package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// ModuleRepository is tenant-scoped: every method takes an institutionID as
// its first argument. Pass PlatformScope (0) only for a platform super_admin.
type ModuleRepository interface {
	Create(institutionID uint, module *models.Module) error
	GetByID(institutionID, id uint) (*models.Module, error)
	Update(institutionID uint, module *models.Module) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.Module, error)
	GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Module, error)
	GetByType(institutionID uint, moduleType models.ModuleType, limit, offset int) ([]models.Module, error)
	GetGeneralModules(institutionID uint, limit, offset int) ([]models.Module, error)
	GetWithStaff(institutionID, id uint) (*models.Module, error)
}

type moduleRepository struct {
	db *gorm.DB
}

func NewModuleRepository(db *gorm.DB) ModuleRepository {
	return &moduleRepository{db: db}
}

func (r *moduleRepository) Create(institutionID uint, module *models.Module) error {
	if module == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		module.InstitutionID = institutionID
	}
	return r.db.Create(module).Error
}

func (r *moduleRepository) GetByID(institutionID, id uint) (*models.Module, error) {
	var module models.Module
	err := TenantScope(r.db, institutionID).Preload("Course").First(&module, id).Error
	if err != nil {
		return nil, err
	}
	return &module, nil
}

func (r *moduleRepository) Update(institutionID uint, module *models.Module) error {
	if module == nil {
		return gorm.ErrInvalidValue
	}
	res := TenantScope(r.db.Model(&models.Module{}), institutionID).
		Where("id = ?", module.ID).
		Updates(map[string]any{
			"name":         module.Name,
			"code":         module.Code,
			"course_id":    module.CourseID,
			"credit_hours": module.CreditHours,
			"type":         module.Type,
			"requires_lab": module.RequiresLab,
			"semester":     module.Semester,
			"nta_level":    module.NtaLevel,
			"updated_at":   gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *moduleRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Module{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *moduleRepository) GetAll(institutionID uint, limit, offset int) ([]models.Module, error) {
	var modules []models.Module
	err := TenantScope(r.db, institutionID).Preload("Course").Limit(limit).Offset(offset).Find(&modules).Error
	return modules, err
}

func (r *moduleRepository) GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Module, error) {
	var modules []models.Module
	err := TenantScope(r.db, institutionID).
		Preload("Course").
		Where("course_id = ?", courseID).
		Limit(limit).Offset(offset).
		Find(&modules).Error
	return modules, err
}

func (r *moduleRepository) GetByType(institutionID uint, moduleType models.ModuleType, limit, offset int) ([]models.Module, error) {
	var modules []models.Module
	err := TenantScope(r.db, institutionID).
		Preload("Course").
		Where("type = ?", moduleType).
		Limit(limit).Offset(offset).
		Find(&modules).Error
	return modules, err
}

// GetGeneralModules returns the institution's own general-subject modules.
// The course_id IS NULL filter used to return every tenant's general
// subjects at once, which would have leaked one institution's curriculum
// into another institution's timetable generation.
func (r *moduleRepository) GetGeneralModules(institutionID uint, limit, offset int) ([]models.Module, error) {
	var modules []models.Module
	err := TenantScope(r.db, institutionID).
		Where("course_id IS NULL").
		Limit(limit).Offset(offset).
		Find(&modules).Error
	return modules, err
}

func (r *moduleRepository) GetWithStaff(institutionID, id uint) (*models.Module, error) {
	var module models.Module
	err := TenantScope(r.db, institutionID).
		Preload("Course").
		Preload("Staff").
		First(&module, id).Error
	if err != nil {
		return nil, err
	}
	return &module, nil
}
