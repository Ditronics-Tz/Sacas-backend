package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

type ModuleRepository interface {
	Create(module *models.Module) error
	GetByID(id uint) (*models.Module, error)
	Update(module *models.Module) error
	Delete(id uint) error
	GetAll(limit, offset int) ([]models.Module, error)
	GetByCourse(courseID uint, limit, offset int) ([]models.Module, error)
	GetByType(moduleType models.ModuleType, limit, offset int) ([]models.Module, error)
	GetGeneralModules(limit, offset int) ([]models.Module, error)
	GetWithStaff(id uint) (*models.Module, error)
}

type moduleRepository struct {
	db *gorm.DB
}

func NewModuleRepository(db *gorm.DB) ModuleRepository {
	return &moduleRepository{db: db}
}

func (r *moduleRepository) Create(module *models.Module) error {
	return r.db.Create(module).Error
}

func (r *moduleRepository) GetByID(id uint) (*models.Module, error) {
	var module models.Module
	err := r.db.Preload("Course").First(&module, id).Error
	if err != nil {
		return nil, err
	}
	return &module, nil
}

func (r *moduleRepository) Update(module *models.Module) error {
	return r.db.Save(module).Error
}

func (r *moduleRepository) Delete(id uint) error {
	return r.db.Delete(&models.Module{}, id).Error
}

func (r *moduleRepository) GetAll(limit, offset int) ([]models.Module, error) {
	var modules []models.Module
	err := r.db.Preload("Course").Limit(limit).Offset(offset).Find(&modules).Error
	return modules, err
}

func (r *moduleRepository) GetByCourse(courseID uint, limit, offset int) ([]models.Module, error) {
	var modules []models.Module
	err := r.db.Preload("Course").Where("course_id = ?", courseID).Limit(limit).Offset(offset).Find(&modules).Error
	return modules, err
}

func (r *moduleRepository) GetByType(moduleType models.ModuleType, limit, offset int) ([]models.Module, error) {
	var modules []models.Module
	err := r.db.Preload("Course").Where("type = ?", moduleType).Limit(limit).Offset(offset).Find(&modules).Error
	return modules, err
}

func (r *moduleRepository) GetGeneralModules(limit, offset int) ([]models.Module, error) {
	var modules []models.Module
	err := r.db.Where("course_id IS NULL").Limit(limit).Offset(offset).Find(&modules).Error
	return modules, err
}

func (r *moduleRepository) GetWithStaff(id uint) (*models.Module, error) {
	var module models.Module
	err := r.db.Preload("Course").Preload("Staff").First(&module, id).Error
	if err != nil {
		return nil, err
	}
	return &module, nil
}