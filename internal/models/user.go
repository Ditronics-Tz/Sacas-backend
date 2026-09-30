package models

import (
	"time"

	"gorm.io/gorm"
)

// UserRole defines the available user roles
type UserRole string

const (
	RoleUser       UserRole = "user"
	RoleAdmin      UserRole = "administrator"
	RoleSuperAdmin UserRole = "super_admin"
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
	Email       string         `gorm:"not null" json:"email" validate:"required,email"`
	Password    string         `gorm:"not null" json:"-" validate:"required"`
	FirstName   string         `gorm:"not null" json:"first_name" validate:"required,min=2,max=50"`
	LastName    string         `gorm:"not null" json:"last_name" validate:"required,min=2,max=50"`
	PhoneNumber string         `json:"phone_number" validate:"omitempty,e164"`
	Role        UserRole       `gorm:"default:'user'" json:"role" validate:"required"`
	IsActive    bool           `gorm:"default:false" json:"is_active"`
	IsVerified  bool           `gorm:"default:false" json:"is_verified"`
	LastLoginAt *time.Time     `json:"last_login_at"`

	// InstitutionID is NULL for platform super_admins (they span all tenants)
	// and set for every other user. Resolved server-side on every request by
	// ActiveUserMiddleware — never from the request body or a stale JWT.
	InstitutionID *uint `gorm:"index" json:"institution_id,omitempty"`

	// Relationships
	Institution *Institution `gorm:"foreignKey:InstitutionID" json:"institution,omitempty"`
	Staff       *Staff       `gorm:"foreignKey:UserID" json:"staff,omitempty"`
}

// IsPlatform reports whether the user is a platform-level super_admin that is
// not bound to a single institution and therefore sees all tenants.
func (u *User) IsPlatform() bool {
	return u.InstitutionID == nil && u.Role == RoleSuperAdmin
}

// InstitutionIDOrDefault returns the user's institution, falling back to the
// default institution when unset. Platform super_admins should use
// IsPlatform instead of relying on this.
func (u *User) InstitutionIDOrDefault() uint {
	if u.InstitutionID == nil {
		return DefaultInstitutionID
	}
	return *u.InstitutionID
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
