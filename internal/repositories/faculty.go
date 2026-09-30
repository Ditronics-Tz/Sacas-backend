package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// FacultyRepository is tenant-scoped: every method takes an institutionID as
// its first argument. Pass PlatformScope (0) only for a platform super_admin.
type FacultyRepository interface {
	Create(institutionID uint, faculty *models.Faculty) error
	GetByID(institutionID, id uint) (*models.Faculty, error)
	Update(institutionID uint, faculty *models.Faculty) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.Faculty, error)
	GetWithCourses(institutionID, id uint) (*models.Faculty, error)
	GetByName(institutionID uint, name string) (*models.Faculty, error)
}

type facultyRepository struct {
	db *gorm.DB
}

func NewFacultyRepository(db *gorm.DB) FacultyRepository {
	return &facultyRepository{db: db}
}

func (r *facultyRepository) Create(institutionID uint, faculty *models.Faculty) error {
	if faculty == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		faculty.InstitutionID = institutionID
	}
	return r.db.Create(faculty).Error
}

func (r *facultyRepository) GetByID(institutionID, id uint) (*models.Faculty, error) {
	var faculty models.Faculty
	// Scoping the query is what makes a cross-tenant read look like a miss.
	err := TenantScope(r.db, institutionID).First(&faculty, id).Error
	if err != nil {
		return nil, err
	}
	return &faculty, nil
}

func (r *facultyRepository) GetByName(institutionID uint, name string) (*models.Faculty, error) {
	var faculty models.Faculty
	err := TenantScope(r.db, institutionID).Where("lower(name) = lower(?)", name).First(&faculty).Error
	if err != nil {
		return nil, err
	}
	return &faculty, nil
}

func (r *facultyRepository) Update(institutionID uint, faculty *models.Faculty) error {
	if faculty == nil {
		return gorm.ErrInvalidValue
	}
	// Guard on the tenant so a caller cannot move a record into another
	// institution by writing its struct, and so a foreign ID affects no rows.
	res := TenantScope(r.db.Model(&models.Faculty{}), institutionID).
		Where("id = ?", faculty.ID).
		Updates(map[string]any{
			"name":        faculty.Name,
			"description": faculty.Description,
			"hod_name":    faculty.HodName,
			"hod_phone":   faculty.HodPhone,
			"hod_email":   faculty.HodEmail,
			"updated_at":  gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *facultyRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Faculty{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *facultyRepository) GetAll(institutionID uint, limit, offset int) ([]models.Faculty, error) {
	var faculties []models.Faculty
	err := TenantScope(r.db, institutionID).Limit(limit).Offset(offset).Find(&faculties).Error
	return faculties, err
}

func (r *facultyRepository) GetWithCourses(institutionID, id uint) (*models.Faculty, error) {
	var faculty models.Faculty
	// Courses are preloaded through the association, which is itself
	// institution-scoped by its own column.
	err := TenantScope(r.db, institutionID).Preload("Courses").First(&faculty, id).Error
	if err != nil {
		return nil, err
	}
	return &faculty, nil
}
