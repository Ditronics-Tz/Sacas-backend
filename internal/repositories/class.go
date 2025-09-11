package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

type ClassRepository interface {
	Create(class *models.Class) error
	GetByID(id uint) (*models.Class, error)
	Update(class *models.Class) error
	Delete(id uint) error
	GetAll(limit, offset int) ([]models.Class, error)
	GetByCourse(courseID uint, limit, offset int) ([]models.Class, error)
	GetByYear(year int, limit, offset int) ([]models.Class, error)
}

type classRepository struct {
	db *gorm.DB
}

func NewClassRepository(db *gorm.DB) ClassRepository {
	return &classRepository{db: db}
}

func (r *classRepository) Create(class *models.Class) error {
	return r.db.Create(class).Error
}

func (r *classRepository) GetByID(id uint) (*models.Class, error) {
	var class models.Class
	err := r.db.Preload("Course").First(&class, id).Error
	if err != nil {
		return nil, err
	}
	return &class, nil
}

func (r *classRepository) Update(class *models.Class) error {
	return r.db.Save(class).Error
}

func (r *classRepository) Delete(id uint) error {
	return r.db.Delete(&models.Class{}, id).Error
}

func (r *classRepository) GetAll(limit, offset int) ([]models.Class, error) {
	var classes []models.Class
	err := r.db.Preload("Course").Limit(limit).Offset(offset).Find(&classes).Error
	return classes, err
}

func (r *classRepository) GetByCourse(courseID uint, limit, offset int) ([]models.Class, error) {
	var classes []models.Class
	err := r.db.Preload("Course").Where("course_id = ?", courseID).Limit(limit).Offset(offset).Find(&classes).Error
	return classes, err
}

func (r *classRepository) GetByYear(year int, limit, offset int) ([]models.Class, error) {
	var classes []models.Class
	err := r.db.Preload("Course").Where("year = ?", year).Limit(limit).Offset(offset).Find(&classes).Error
	return classes, err
}