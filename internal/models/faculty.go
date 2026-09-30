package models

import (
	"time"

	"gorm.io/gorm"
)

type Faculty struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"not null" json:"name" validate:"required,min=2,max=100"`
	Description string         `json:"description"`
	HodName     string         `json:"hod_name"`
	HodPhone    string         `json:"hod_phone"`
	HodEmail    string         `json:"hod_email"`

	// InstitutionID is the owning tenant. Faculty names are unique per
	// institution, not globally.
	InstitutionID uint `gorm:"not null;index" json:"institution_id"`

	// Relationships
	Institution Institution `gorm:"foreignKey:InstitutionID" json:"institution,omitempty"`
	Courses     []Course    `json:"courses,omitempty"`
	Staff       []Staff     `json:"staff,omitempty"`
}
