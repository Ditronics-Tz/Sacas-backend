package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

type CourseRepository interface {
	Create(course *models.Course) error
	GetByID(id uint) (*models.Course, error)
	Update(course *models.Course) error
	Delete(id uint) error
	GetAll(limit, offset int) ([]models.Course, error)
	GetByFaculty(facultyID uint, limit, offset int) ([]models.Course, error)
	GetWithModules(id uint) (*models.Course, error)
}

type courseRepository struct {
	db *gorm.DB
}

func NewCourseRepository(db *gorm.DB) CourseRepository {
	return &courseRepository{db: db}
}

func (r *courseRepository) Create(course *models.Course) error {
	return r.db.Create(course).Error
}

func (r *courseRepository) GetByID(id uint) (*models.Course, error) {
	var course models.Course
	err := r.db.Preload("Faculty").First(&course, id).Error
	if err != nil {
		return nil, err
	}
	return &course, nil
}

func (r *courseRepository) Update(course *models.Course) error {
	return r.db.Save(course).Error
}

func (r *courseRepository) Delete(id uint) error {
	return r.db.Delete(&models.Course{}, id).Error
}

func (r *courseRepository) GetAll(limit, offset int) ([]models.Course, error) {
	var courses []models.Course
	err := r.db.Preload("Faculty").Limit(limit).Offset(offset).Find(&courses).Error
	return courses, err
}

func (r *courseRepository) GetByFaculty(facultyID uint, limit, offset int) ([]models.Course, error) {
	var courses []models.Course
	err := r.db.Preload("Faculty").Where("faculty_id = ?", facultyID).Limit(limit).Offset(offset).Find(&courses).Error
	return courses, err
}

func (r *courseRepository) GetWithModules(id uint) (*models.Course, error) {
	var course models.Course
	err := r.db.Preload("Faculty").Preload("Modules").First(&course, id).Error
	if err != nil {
		return nil, err
	}
	return &course, nil
}