package models

import (
	"time"

	"gorm.io/gorm"
)

// UserRole defines the available user roles.
//
// Roles split into two families:
//
//	Platform   — super_admin, not bound to an institution. Operates across all
//	             tenants and manages institutions themselves.
//	Institution — every other role, always bound to exactly one institution.
//
// The two new coordinator roles narrow `administrator` rather than extending it:
// an academic coordinator runs the teaching timetable, an exam coordinator runs
// exams. Neither can administer users, and each is confined to its own domain.
type UserRole string

const (
	// Platform role. Not bound to an institution.
	RoleSuperAdmin UserRole = "super_admin"

	// Institution-scoped roles.
	RoleUser          UserRole = "user"          // lecturer / staff
	RoleAdmin         UserRole = "administrator" // institution admin
	RoleAcademicCoord UserRole = "academic_coordinator"
	RoleExamCoord     UserRole = "exam_coordinator"

	// RoleSupport is not assignable to a person. It is the synthetic role
	// carried by a support-access (impersonation) token, and it holds read
	// permissions only. Because it is an institution role, a support token
	// carries a real institution scope and the normal tenant scoping applies to
	// it unchanged.
	RoleSupport UserRole = "institution_support"
)

// AllRoles lists every valid role, ordered from most to least privileged. It
// backs validation and the permission tests, so adding a role here is the
// reminder to also give it a permission set.
//
// RoleSupport is deliberately included: it must satisfy the same validation, but
// AssignableRoles never offers it, so nobody can be given it permanently.
var AllRoles = []UserRole{
	RoleSuperAdmin,
	RoleAdmin,
	RoleAcademicCoord,
	RoleExamCoord,
	RoleUser,
	RoleSupport,
}

// IsValid checks if the role is valid
func (r UserRole) IsValid() bool {
	for _, known := range AllRoles {
		if r == known {
			return true
		}
	}
	return false
}

// IsPlatformRole reports whether the role is not bound to a single institution.
func (r UserRole) IsPlatformRole() bool { return r == RoleSuperAdmin }

// IsInstitutionRole reports whether the role requires an institution. Every
// role except super_admin does, which is why a super_admin with no institution
// has nothing to work with in an institution workspace.
func (r UserRole) IsInstitutionRole() bool {
	return r != RoleSuperAdmin
}

// IsSupportRole reports whether the role belongs to a support-access token
// rather than to a person. A support session is read-only and cannot be
// granted to a real account.
func (r UserRole) IsSupportRole() bool { return r == RoleSupport }

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
	// TenantMiddleware — never from the request body or a stale JWT.
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

// HasInstitution reports whether the user is bound to an institution and can
// therefore act in an institution workspace.
func (u *User) HasInstitution() bool { return u.InstitutionID != nil }

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

// CanAccessAdminFeatures reports whether the user can reach institution
// administration surfaces. It is deliberately narrower than it looks: the
// authoritative check is the permission map in internal/auth, not this helper.
func (u *User) CanAccessAdminFeatures() bool {
	return u.Role == RoleAdmin || u.Role == RoleSuperAdmin
}

// CanAccessSuperAdminFeatures checks if user can access super admin features
func (u *User) CanAccessSuperAdminFeatures() bool {
	return u.Role == RoleSuperAdmin
}
