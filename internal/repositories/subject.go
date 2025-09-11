package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

type SubjectRepository interface {
	Create(subject *models.Subject) error
	GetByID(id uint) (*models.Subject, error)
	Update(subject *models.Subject) error
	Delete(id uint) error
	GetAll(limit, offset int) ([]models.Subject, error)
	GetByCreditHours(creditHours int) ([]models.Subject, error)
}

type subjectRepository struct {
	db *gorm.DB
}

func NewSubjectRepository(db *gorm.DB) SubjectRepository {
	return &subjectRepository{db: db}
}

func (r *subjectRepository) Create(subject *models.Subject) error {
	return r.db.Create(subject).Error
}

func (r *subjectRepository) GetByID(id uint) (*models.Subject, error) {
	var subject models.Subject
	err := r.db.First(&subject, id).Error
	if err != nil {
		return nil, err
	}
	return &subject, nil
}

func (r *subjectRepository) Update(subject *models.Subject) error {
	return r.db.Save(subject).Error
}

func (r *subjectRepository) Delete(id uint) error {
	return r.db.Delete(&models.Subject{}, id).Error
}

func (r *subjectRepository) GetAll(limit, offset int) ([]models.Subject, error) {
	var subjects []models.Subject
	err := r.db.Limit(limit).Offset(offset).Find(&subjects).Error
	return subjects, err
}

func (r *subjectRepository) GetByCreditHours(creditHours int) ([]models.Subject, error) {
	var subjects []models.Subject
	err := r.db.Where("credit_hours = ?", creditHours).Find(&subjects).Error
	return subjects, err
}