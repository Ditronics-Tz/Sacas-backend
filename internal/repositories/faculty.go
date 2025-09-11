package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

type FacultyRepository interface {
	Create(faculty *models.Faculty) error
	GetByID(id uint) (*models.Faculty, error)
	Update(faculty *models.Faculty) error
	Delete(id uint) error
	GetAll(limit, offset int) ([]models.Faculty, error)
	GetWithCourses(id uint) (*models.Faculty, error)
}

type facultyRepository struct {
	db *gorm.DB
}

func NewFacultyRepository(db *gorm.DB) FacultyRepository {
	return &facultyRepository{db: db}
}

func (r *facultyRepository) Create(faculty *models.Faculty) error {
	return r.db.Create(faculty).Error
}

func (r *facultyRepository) GetByID(id uint) (*models.Faculty, error) {
	var faculty models.Faculty
	err := r.db.First(&faculty, id).Error
	if err != nil {
		return nil, err
	}
	return &faculty, nil
}

func (r *facultyRepository) Update(faculty *models.Faculty) error {
	return r.db.Save(faculty).Error
}

func (r *facultyRepository) Delete(id uint) error {
	return r.db.Delete(&models.Faculty{}, id).Error
}

func (r *facultyRepository) GetAll(limit, offset int) ([]models.Faculty, error) {
	var faculties []models.Faculty
	err := r.db.Limit(limit).Offset(offset).Find(&faculties).Error
	return faculties, err
}

func (r *facultyRepository) GetWithCourses(id uint) (*models.Faculty, error) {
	var faculty models.Faculty
	err := r.db.Preload("Courses").First(&faculty, id).Error
	if err != nil {
		return nil, err
	}
	return &faculty, nil
}