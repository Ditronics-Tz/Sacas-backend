package models

import (
	"time"

	"gorm.io/gorm"
)

// InstitutionType is the broad category of a tenant.
type InstitutionType string

const (
	InstitutionTypeCollege    InstitutionType = "college"
	InstitutionTypeUniversity InstitutionType = "university"
	InstitutionTypeSchool     InstitutionType = "school"
	InstitutionTypeInstitute  InstitutionType = "institute"
)

func (t InstitutionType) IsValid() bool {
	switch t {
	case InstitutionTypeCollege, InstitutionTypeUniversity, InstitutionTypeSchool, InstitutionTypeInstitute:
		return true
	}
	return false
}

// InstitutionStatus is the lifecycle state of a tenant. Only active
// institutions may be used by tenant users; pending/suspended institutions
// are visible to platform super_admins only.
type InstitutionStatus string

const (
	InstitutionStatusPending   InstitutionStatus = "pending"
	InstitutionStatusActive    InstitutionStatus = "active"
	InstitutionStatusSuspended InstitutionStatus = "suspended"
)

func (s InstitutionStatus) IsValid() bool {
	switch s {
	case InstitutionStatusPending, InstitutionStatusActive, InstitutionStatusSuspended:
		return true
	}
	return false
}

// IsUsable reports whether tenants of this institution may sign in and act.
func (s InstitutionStatus) IsUsable() bool { return s == InstitutionStatusActive }

// InstitutionPlan is the subscription tier of a tenant.
type InstitutionPlan string

const (
	InstitutionPlanFree       InstitutionPlan = "free"
	InstitutionPlanPro        InstitutionPlan = "pro"
	InstitutionPlanEnterprise InstitutionPlan = "enterprise"
)

func (p InstitutionPlan) IsValid() bool {
	switch p {
	case InstitutionPlanFree, InstitutionPlanPro, InstitutionPlanEnterprise:
		return true
	}
	return false
}

// Institution is the tenant root. Every timetable-domain record belongs to
// exactly one institution, and users are either platform staff
// (User.InstitutionID == nil) or scoped to exactly one institution.
type Institution struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Name    string          `gorm:"not null" json:"name" validate:"required,min=2,max=150"`
	Slug    string          `gorm:"not null" json:"slug" validate:"required,min=2,max=150"`
	Type    InstitutionType `gorm:"not null;default:'college'" json:"type" validate:"required"`
	Region  string          `json:"region"`
	Country string          `gorm:"default:'Tanzania'" json:"country"`
	Email   string          `json:"email" validate:"omitempty,email"`
	Phone   string          `json:"phone" validate:"omitempty,e164"`
	Address string          `json:"address"`
	LogoURL string          `json:"logo_url"`

	Status      InstitutionStatus `gorm:"not null;default:'pending'" json:"status" validate:"required"`
	Plan        InstitutionPlan   `gorm:"not null;default:'free'" json:"plan" validate:"required"`
	TrialEndsAt *time.Time        `json:"trial_ends_at"`

	// Relationships. These are tenant-scoped associations; callers must always
	// go through the repository with the tenant scope applied.
	Users     []User    `json:"users,omitempty"`
	Faculties []Faculty `json:"faculties,omitempty"`
	Courses   []Course  `json:"courses,omitempty"`
	Classes   []Class   `json:"classes,omitempty"`
	Staff     []Staff   `json:"staff,omitempty"`
	Rooms     []Room    `json:"rooms,omitempty"`
	Subjects  []Subject `json:"subjects,omitempty"`
}

// TrialExpired reports whether a trial has lapsed at the given time. A nil
// TrialEndsAt means the institution is not on a trial.
func (i *Institution) TrialExpired(now time.Time) bool {
	return i.TrialEndsAt != nil && i.TrialEndsAt.Before(now)
}

// IsActive reports whether the institution may serve requests right now.
// Suspended and expired-trial institutions are rejected.
func (i *Institution) IsActive(now time.Time) bool {
	return i.Status.IsUsable() && !i.TrialExpired(now)
}

// DefaultInstitutionID is the fixed primary key of the tenant every existing
// installation is backfilled into. It is also the tenant used by demo seeding
// so a fresh local install has somewhere to put data.
const DefaultInstitutionID uint = 1

// DefaultInstitutionName is the display name of the backfill tenant.
const DefaultInstitutionName = "Default Institution"
