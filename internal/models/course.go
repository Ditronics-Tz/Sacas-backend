package models

import (
	"time"

	"gorm.io/gorm"
)

type Course struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"not null" json:"name" validate:"required,min=2,max=100"`
	FacultyID   uint           `gorm:"not null" json:"faculty_id" validate:"required"`
	Description string         `json:"description"`
	Level       string         `json:"level"` // e.g. diploma, degree, NTA Level 6

	// Relationships
	Faculty Faculty  `gorm:"foreignKey:FacultyID" json:"faculty,omitempty"`
	Modules []Module `json:"modules,omitempty"`
	Classes []Class  `json:"classes,omitempty"`
}
