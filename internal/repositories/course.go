package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// CourseRepository is tenant-scoped: every method takes an institutionID as
// its first argument. Pass PlatformScope (0) only for a platform super_admin.
type CourseRepository interface {
	Create(institutionID uint, course *models.Course) error
	GetByID(institutionID, id uint) (*models.Course, error)
	Update(institutionID uint, course *models.Course) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.Course, error)
	GetByFaculty(institutionID, facultyID uint, limit, offset int) ([]models.Course, error)
	GetWithModules(institutionID, id uint) (*models.Course, error)
	GetByName(institutionID uint, name string) (*models.Course, error)
}

type courseRepository struct {
	db *gorm.DB
}

func NewCourseRepository(db *gorm.DB) CourseRepository {
	return &courseRepository{db: db}
}

func (r *courseRepository) Create(institutionID uint, course *models.Course) error {
	if course == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		course.InstitutionID = institutionID
	}
	return r.db.Create(course).Error
}

func (r *courseRepository) GetByID(institutionID, id uint) (*models.Course, error) {
	var course models.Course
	err := TenantScope(r.db, institutionID).Preload("Faculty").First(&course, id).Error
	if err != nil {
		return nil, err
	}
	return &course, nil
}

func (r *courseRepository) GetByName(institutionID uint, name string) (*models.Course, error) {
	var course models.Course
	err := TenantScope(r.db, institutionID).Where("lower(name) = lower(?)", name).First(&course).Error
	if err != nil {
		return nil, err
	}
	return &course, nil
}

func (r *courseRepository) Update(institutionID uint, course *models.Course) error {
	if course == nil {
		return gorm.ErrInvalidValue
	}
	res := TenantScope(r.db.Model(&models.Course{}), institutionID).
		Where("id = ?", course.ID).
		Updates(map[string]any{
			"name":        course.Name,
			"faculty_id":  course.FacultyID,
			"description": course.Description,
			"level":       course.Level,
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

func (r *courseRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Course{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *courseRepository) GetAll(institutionID uint, limit, offset int) ([]models.Course, error) {
	var courses []models.Course
	err := TenantScope(r.db, institutionID).Preload("Faculty").Limit(limit).Offset(offset).Find(&courses).Error
	return courses, err
}

func (r *courseRepository) GetByFaculty(institutionID, facultyID uint, limit, offset int) ([]models.Course, error) {
	var courses []models.Course
	err := TenantScope(r.db, institutionID).
		Preload("Faculty").
		Where("faculty_id = ?", facultyID).
		Limit(limit).Offset(offset).
		Find(&courses).Error
	return courses, err
}

func (r *courseRepository) GetWithModules(institutionID, id uint) (*models.Course, error) {
	var course models.Course
	err := TenantScope(r.db, institutionID).
		Preload("Faculty").
		Preload("Modules").
		First(&course, id).Error
	if err != nil {
		return nil, err
	}
	return &course, nil
}
