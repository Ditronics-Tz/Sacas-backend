package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

type StaffRepository interface {
	Create(staff *models.Staff) error
	GetByID(id uint) (*models.Staff, error)
	GetByEmail(email string) (*models.Staff, error)
	Update(staff *models.Staff) error
	Delete(id uint) error
	GetAll(limit, offset int) ([]models.Staff, error)
	GetByFaculty(facultyID uint, limit, offset int) ([]models.Staff, error)
	GetWithModules(id uint) (*models.Staff, error)
	UpdatePreferences(id uint, preferences string) error
	AssignModule(staffID, moduleID uint) error
	UnassignModule(staffID, moduleID uint) error
	ListModules(staffID uint) ([]models.Module, error)
	ListStaffForModule(moduleID uint) ([]models.Staff, error)
}

type staffRepository struct {
	db *gorm.DB
}

func NewStaffRepository(db *gorm.DB) StaffRepository {
	return &staffRepository{db: db}
}

func (r *staffRepository) Create(staff *models.Staff) error {
	return r.db.Create(staff).Error
}

func (r *staffRepository) GetByID(id uint) (*models.Staff, error) {
	var staff models.Staff
	err := r.db.Preload("Faculty").First(&staff, id).Error
	if err != nil {
		return nil, err
	}
	return &staff, nil
}

func (r *staffRepository) GetByEmail(email string) (*models.Staff, error) {
	var staff models.Staff
	err := r.db.Preload("Faculty").Where("email = ?", email).First(&staff).Error
	if err != nil {
		return nil, err
	}
	return &staff, nil
}

func (r *staffRepository) Update(staff *models.Staff) error {
	return r.db.Save(staff).Error
}

func (r *staffRepository) Delete(id uint) error {
	return r.db.Delete(&models.Staff{}, id).Error
}

func (r *staffRepository) GetAll(limit, offset int) ([]models.Staff, error) {
	var staff []models.Staff
	err := r.db.Preload("Faculty").Limit(limit).Offset(offset).Find(&staff).Error
	return staff, err
}

func (r *staffRepository) GetByFaculty(facultyID uint, limit, offset int) ([]models.Staff, error) {
	var staff []models.Staff
	err := r.db.Preload("Faculty").Where("faculty_id = ?", facultyID).Limit(limit).Offset(offset).Find(&staff).Error
	return staff, err
}

func (r *staffRepository) GetWithModules(id uint) (*models.Staff, error) {
	var staff models.Staff
	err := r.db.Preload("Faculty").Preload("Modules").First(&staff, id).Error
	if err != nil {
		return nil, err
	}
	return &staff, nil
}

func (r *staffRepository) UpdatePreferences(id uint, preferences string) error {
	return r.db.Model(&models.Staff{}).Where("id = ?", id).Update("preferences", preferences).Error
}

func (r *staffRepository) AssignModule(staffID, moduleID uint) error {
	staff := models.Staff{ID: staffID}
	module := models.Module{ID: moduleID}
	return r.db.Model(&staff).Association("Modules").Append(&module)
}

func (r *staffRepository) UnassignModule(staffID, moduleID uint) error {
	staff := models.Staff{ID: staffID}
	module := models.Module{ID: moduleID}
	return r.db.Model(&staff).Association("Modules").Delete(&module)
}

func (r *staffRepository) ListModules(staffID uint) ([]models.Module, error) {
	staff := models.Staff{ID: staffID}
	var modules []models.Module
	err := r.db.Model(&staff).Association("Modules").Find(&modules)
	return modules, err
}

func (r *staffRepository) ListStaffForModule(moduleID uint) ([]models.Staff, error) {
	module := models.Module{ID: moduleID}
	var staff []models.Staff
	err := r.db.Model(&module).Association("Staff").Find(&staff)
	return staff, err
}
