package models

import (
	"time"

	"gorm.io/gorm"
)

// UserRole defines the available user roles
type UserRole string

const (
	RoleUser        UserRole = "user"
	RoleAdmin       UserRole = "administrator"
	RoleSuperAdmin  UserRole = "super_admin"
)

// IsValid checks if the role is valid
func (r UserRole) IsValid() bool {
	return r == RoleUser || r == RoleAdmin || r == RoleSuperAdmin
}

type User struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Email       string         `gorm:"unique;not null" json:"email" validate:"required,email"`
	Password    string         `gorm:"not null" json:"-" validate:"required,min=6"`
	FirstName   string         `gorm:"not null" json:"first_name" validate:"required,min=2,max=50"`
	LastName    string         `gorm:"not null" json:"last_name" validate:"required,min=2,max=50"`
	PhoneNumber string         `gorm:"unique" json:"phone_number" validate:"omitempty,e164"`
	Role        UserRole       `gorm:"default:'user'" json:"role" validate:"required"`
	IsActive    bool           `gorm:"default:false" json:"is_active"`
	IsVerified  bool           `gorm:"default:false" json:"is_verified"`
	LastLoginAt *time.Time     `json:"last_login_at"`
}

// GetFullName returns the user's full name
func (u *User) GetFullName() string {
	return u.FirstName + " " + u.LastName
}

// HasRole checks if the user has the specified role
func (u *User) HasRole(role UserRole) bool {
	return u.Role == role
}

// CanAccessAdminFeatures checks if user can access admin features
func (u *User) CanAccessAdminFeatures() bool {
	return u.Role == RoleAdmin || u.Role == RoleSuperAdmin
}

// CanAccessSuperAdminFeatures checks if user can access super admin features
func (u *User) CanAccessSuperAdminFeatures() bool {
	return u.Role == RoleSuperAdmin
}