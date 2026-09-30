package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// UserRepository is tenant-scoped for everything except authentication.
//
// Email stays globally unique across the whole users table, so the login
// lookup (GetByEmail) takes no institutionID — a login resolves to exactly one
// account, and that account's institution_id then determines what the user can
// see. The listing and mutation methods are tenant-scoped so one institution's
// admin cannot enumerate or edit another institution's users.
type UserRepository interface {
	Create(user *models.User) error
	GetByID(id uint) (*models.User, error)
	GetByEmail(email string) (*models.User, error)
	Update(institutionID uint, user *models.User) error
	// UpdateSelf updates a user using that user's OWN institution as the
	// scope. It is for the authentication flows (login timestamp, email
	// verification) where the acting user is the subject and no tenant has
	// been resolved in the request context yet.
	UpdateSelf(user *models.User) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.User, error)
	GetByRole(institutionID uint, role string, limit, offset int) ([]models.User, error)
	CountByInstitution(institutionID uint) (int64, error)
	UpdatePassword(id uint, hashedPassword string) error
	UpdateRole(institutionID, id uint, role string) error
	ActivateUser(institutionID, id uint) error
	DeactivateUser(institutionID, id uint) error
}

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(user *models.User) error {
	return r.db.Create(user).Error
}

// GetByID is unscoped by design: it backs the auth middleware, which has to
// resolve the caller's own row before the tenant is known. The tenant is read
// from this row and never from the request.
func (r *userRepository) GetByID(id uint) (*models.User, error) {
	var user models.User
	err := r.db.First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetByEmail is unscoped by design: it is the login lookup. Email is globally
// unique, so there is exactly one match and no tenant guesswork is involved.
func (r *userRepository) GetByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.db.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) Update(institutionID uint, user *models.User) error {
	if user == nil {
		return gorm.ErrInvalidValue
	}
	// institution_id is excluded on purpose: a user must not be moved between
	// institutions by a plain profile update.
	res := TenantScope(r.db.Model(&models.User{}), institutionID).
		Where("id = ?", user.ID).
		Updates(map[string]any{
			"email":        user.Email,
			"first_name":   user.FirstName,
			"last_name":    user.LastName,
			"phone_number": user.PhoneNumber,
			"role":         user.Role,
			"is_active":    user.IsActive,
			"is_verified":  user.IsVerified,
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

// UpdateSelf scopes the update to the user's own institution. A platform
// super_admin (institution_id NULL) resolves to PlatformScope, which is an
// unscoped update matching exactly on the primary key.
func (r *userRepository) UpdateSelf(user *models.User) error {
	if user == nil {
		return gorm.ErrInvalidValue
	}
	scope := PlatformScope
	if user.InstitutionID != nil {
		scope = *user.InstitutionID
	}
	return r.Update(scope, user)
}

func (r *userRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.User{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *userRepository) GetAll(institutionID uint, limit, offset int) ([]models.User, error) {
	var users []models.User
	// Platform super_admins (institution_id IS NULL) belong to no institution,
	// so a tenant-scoped listing must exclude them explicitly.
	query := r.db.Model(&models.User{}).Where("institution_id IS NOT NULL")
	if !IsPlatformScope(institutionID) {
		query = query.Where("institution_id = ?", institutionID)
	}
	err := query.Limit(limit).Offset(offset).Find(&users).Error
	return users, err
}

func (r *userRepository) GetByRole(institutionID uint, role string, limit, offset int) ([]models.User, error) {
	var users []models.User
	query := r.db.Model(&models.User{}).
		Where("role = ?", role).
		Where("institution_id IS NOT NULL")
	if !IsPlatformScope(institutionID) {
		query = query.Where("institution_id = ?", institutionID)
	}
	err := query.Limit(limit).Offset(offset).Find(&users).Error
	return users, err
}

func (r *userRepository) CountByInstitution(institutionID uint) (int64, error) {
	var count int64
	query := r.db.Model(&models.User{}).Where("institution_id IS NOT NULL")
	if !IsPlatformScope(institutionID) {
		query = query.Where("institution_id = ?", institutionID)
	}
	err := query.Count(&count).Error
	return count, err
}

func (r *userRepository) UpdatePassword(id uint, hashedPassword string) error {
	return r.db.Model(&models.User{}).Where("id = ?", id).Update("password", hashedPassword).Error
}

func (r *userRepository) UpdateRole(institutionID, id uint, role string) error {
	res := TenantScope(r.db.Model(&models.User{}), institutionID).
		Where("id = ?", id).
		Update("role", role)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *userRepository) ActivateUser(institutionID, id uint) error {
	res := TenantScope(r.db.Model(&models.User{}), institutionID).
		Where("id = ?", id).
		Update("is_active", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *userRepository) DeactivateUser(institutionID, id uint) error {
	res := TenantScope(r.db.Model(&models.User{}), institutionID).
		Where("id = ?", id).
		Update("is_active", false)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
