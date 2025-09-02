package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

type UserRepository interface {
	Create(user *models.User) error
	GetByID(id uint) (*models.User, error)
	GetByEmail(email string) (*models.User, error)
	Update(user *models.User) error
	Delete(id uint) error
	GetAll(limit, offset int) ([]models.User, error)
	GetByRole(role string, limit, offset int) ([]models.User, error)
	UpdatePassword(id uint, hashedPassword string) error
	UpdateRole(id uint, role string) error
	ActivateUser(id uint) error
	DeactivateUser(id uint) error
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

func (r *userRepository) GetByID(id uint) (*models.User, error) {
	var user models.User
	err := r.db.First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) GetByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.db.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) Update(user *models.User) error {
	return r.db.Save(user).Error
}

func (r *userRepository) Delete(id uint) error {
	return r.db.Delete(&models.User{}, id).Error
}

func (r *userRepository) GetAll(limit, offset int) ([]models.User, error) {
	var users []models.User
	err := r.db.Limit(limit).Offset(offset).Find(&users).Error
	return users, err
}

func (r *userRepository) GetByRole(role string, limit, offset int) ([]models.User, error) {
	var users []models.User
	err := r.db.Where("role = ?", role).Limit(limit).Offset(offset).Find(&users).Error
	return users, err
}

func (r *userRepository) UpdatePassword(id uint, hashedPassword string) error {
	return r.db.Model(&models.User{}).Where("id = ?", id).Update("password", hashedPassword).Error
}

func (r *userRepository) UpdateRole(id uint, role string) error {
	return r.db.Model(&models.User{}).Where("id = ?", id).Update("role", role).Error
}

func (r *userRepository) ActivateUser(id uint) error {
	return r.db.Model(&models.User{}).Where("id = ?", id).Update("is_active", true).Error
}

func (r *userRepository) DeactivateUser(id uint) error {
	return r.db.Model(&models.User{}).Where("id = ?", id).Update("is_active", false).Error
}