package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// InstitutionRepository manages tenants themselves. It is NOT tenant-scoped:
// only a platform super_admin may read or write institutions, and the
// middleware/method names make that explicit rather than implicit.
type InstitutionRepository interface {
	Create(institution *models.Institution) error
	GetByID(id uint) (*models.Institution, error)
	GetBySlug(slug string) (*models.Institution, error)
	Update(institution *models.Institution) error
	Delete(id uint) error
	GetAll(limit, offset int) ([]models.Institution, error)
	CountAll() (int64, error)
	GetByStatus(status models.InstitutionStatus, limit, offset int) ([]models.Institution, error)
	CountByStatus(status models.InstitutionStatus) (int64, error)
	// CountUsers returns the number of non-deleted users in the institution.
	CountUsers(id uint) (int64, error)
}

type institutionRepository struct {
	db *gorm.DB
}

func NewInstitutionRepository(db *gorm.DB) InstitutionRepository {
	return &institutionRepository{db: db}
}

func (r *institutionRepository) Create(institution *models.Institution) error {
	if institution == nil {
		return gorm.ErrInvalidValue
	}
	return r.db.Create(institution).Error
}

func (r *institutionRepository) GetByID(id uint) (*models.Institution, error) {
	var institution models.Institution
	if err := r.db.First(&institution, id).Error; err != nil {
		return nil, err
	}
	return &institution, nil
}

func (r *institutionRepository) GetBySlug(slug string) (*models.Institution, error) {
	var institution models.Institution
	if err := r.db.Where("lower(slug) = lower(?)", slug).First(&institution).Error; err != nil {
		return nil, err
	}
	return &institution, nil
}

func (r *institutionRepository) Update(institution *models.Institution) error {
	if institution == nil {
		return gorm.ErrInvalidValue
	}
	res := r.db.Model(&models.Institution{}).
		Where("id = ?", institution.ID).
		Updates(map[string]any{
			"name":          institution.Name,
			"slug":          institution.Slug,
			"type":          institution.Type,
			"region":        institution.Region,
			"country":       institution.Country,
			"email":         institution.Email,
			"phone":         institution.Phone,
			"address":       institution.Address,
			"logo_url":      institution.LogoURL,
			"status":        institution.Status,
			"plan":          institution.Plan,
			"trial_ends_at": institution.TrialEndsAt,
			"updated_at":    gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *institutionRepository) Delete(id uint) error {
	res := r.db.Delete(&models.Institution{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *institutionRepository) GetAll(limit, offset int) ([]models.Institution, error) {
	var institutions []models.Institution
	err := r.db.Limit(limit).Offset(offset).Find(&institutions).Error
	return institutions, err
}

func (r *institutionRepository) CountAll() (int64, error) {
	var count int64
	err := r.db.Model(&models.Institution{}).Count(&count).Error
	return count, err
}

func (r *institutionRepository) GetByStatus(status models.InstitutionStatus, limit, offset int) ([]models.Institution, error) {
	var institutions []models.Institution
	err := r.db.Where("status = ?", status).Limit(limit).Offset(offset).Find(&institutions).Error
	return institutions, err
}

func (r *institutionRepository) CountByStatus(status models.InstitutionStatus) (int64, error) {
	var count int64
	err := r.db.Model(&models.Institution{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

func (r *institutionRepository) CountUsers(id uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.User{}).
		Where("institution_id = ? AND deleted_at IS NULL", id).
		Count(&count).Error
	return count, err
}
