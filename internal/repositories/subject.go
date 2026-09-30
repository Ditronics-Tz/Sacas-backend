package repositories

import (
	"go_boilerplate/internal/models"

	"gorm.io/gorm"
)

// SubjectRepository is tenant-scoped: every method takes an institutionID as
// its first argument. Pass PlatformScope (0) only for a platform super_admin.
type SubjectRepository interface {
	Create(institutionID uint, subject *models.Subject) error
	GetByID(institutionID, id uint) (*models.Subject, error)
	Update(institutionID uint, subject *models.Subject) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.Subject, error)
	GetByCreditHours(institutionID uint, creditHours int) ([]models.Subject, error)
}

type subjectRepository struct {
	db *gorm.DB
}

func NewSubjectRepository(db *gorm.DB) SubjectRepository {
	return &subjectRepository{db: db}
}

func (r *subjectRepository) Create(institutionID uint, subject *models.Subject) error {
	if subject == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		subject.InstitutionID = institutionID
	}
	return r.db.Create(subject).Error
}

func (r *subjectRepository) GetByID(institutionID, id uint) (*models.Subject, error) {
	var subject models.Subject
	err := TenantScope(r.db, institutionID).First(&subject, id).Error
	if err != nil {
		return nil, err
	}
	return &subject, nil
}

func (r *subjectRepository) Update(institutionID uint, subject *models.Subject) error {
	if subject == nil {
		return gorm.ErrInvalidValue
	}
	res := TenantScope(r.db.Model(&models.Subject{}), institutionID).
		Where("id = ?", subject.ID).
		Updates(map[string]any{
			"name":         subject.Name,
			"credit_hours": subject.CreditHours,
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

func (r *subjectRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Subject{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *subjectRepository) GetAll(institutionID uint, limit, offset int) ([]models.Subject, error) {
	var subjects []models.Subject
	err := TenantScope(r.db, institutionID).Limit(limit).Offset(offset).Find(&subjects).Error
	return subjects, err
}

func (r *subjectRepository) GetByCreditHours(institutionID uint, creditHours int) ([]models.Subject, error) {
	var subjects []models.Subject
	err := TenantScope(r.db, institutionID).Where("credit_hours = ?", creditHours).Find(&subjects).Error
	return subjects, err
}
