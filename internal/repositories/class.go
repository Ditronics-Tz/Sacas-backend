package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// ClassRepository is tenant-scoped: every method takes an institutionID as
// its first argument. Pass PlatformScope (0) only for a platform super_admin.
type ClassRepository interface {
	Create(institutionID uint, class *models.Class) error
	GetByID(institutionID, id uint) (*models.Class, error)
	Update(institutionID uint, class *models.Class) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.Class, error)
	GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Class, error)
	GetByYear(institutionID uint, year int, limit, offset int) ([]models.Class, error)
}

type classRepository struct {
	db *gorm.DB
}

func NewClassRepository(db *gorm.DB) ClassRepository {
	return &classRepository{db: db}
}

func (r *classRepository) Create(institutionID uint, class *models.Class) error {
	if class == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		class.InstitutionID = institutionID
	}
	return r.db.Create(class).Error
}

func (r *classRepository) GetByID(institutionID, id uint) (*models.Class, error) {
	var class models.Class
	err := TenantScope(r.db, institutionID).Preload("Course").First(&class, id).Error
	if err != nil {
		return nil, err
	}
	return &class, nil
}

func (r *classRepository) Update(institutionID uint, class *models.Class) error {
	if class == nil {
		return gorm.ErrInvalidValue
	}
	res := TenantScope(r.db.Model(&models.Class{}), institutionID).
		Where("id = ?", class.ID).
		Updates(map[string]any{
			"name":               class.Name,
			"course_id":          class.CourseID,
			"year":               class.Year,
			"academic_year":      class.AcademicYear,
			"number_of_students": class.NumberOfStudents,
			"updated_at":         gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *classRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Class{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *classRepository) GetAll(institutionID uint, limit, offset int) ([]models.Class, error) {
	var classes []models.Class
	err := TenantScope(r.db, institutionID).Preload("Course").Limit(limit).Offset(offset).Find(&classes).Error
	return classes, err
}

func (r *classRepository) GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Class, error) {
	var classes []models.Class
	err := TenantScope(r.db, institutionID).
		Preload("Course").
		Where("course_id = ?", courseID).
		Limit(limit).Offset(offset).
		Find(&classes).Error
	return classes, err
}

func (r *classRepository) GetByYear(institutionID uint, year int, limit, offset int) ([]models.Class, error) {
	var classes []models.Class
	err := TenantScope(r.db, institutionID).
		Preload("Course").
		Where("year = ?", year).
		Limit(limit).Offset(offset).
		Find(&classes).Error
	return classes, err
}
