package models

import (
	"time"

	"gorm.io/gorm"
)

type ModuleType string

const (
	ModuleTypeCore     ModuleType = "core"
	ModuleTypeElective ModuleType = "elective"
	ModuleTypeGeneral  ModuleType = "general_subject"
)

// IsValid checks if the module type is valid
func (m ModuleType) IsValid() bool {
	return m == ModuleTypeCore || m == ModuleTypeElective || m == ModuleTypeGeneral
}

type Module struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
	Name         string         `gorm:"not null" json:"name" validate:"required,min=2,max=100"`
	CourseID     *uint          `json:"course_id"` // Nullable for general subjects
	CreditHours  int            `gorm:"not null" json:"credit_hours" validate:"required,min=1,max=10"`
	Type         ModuleType     `gorm:"not null" json:"type" validate:"required"`
	RequiresLab  bool           `gorm:"default:false" json:"requires_lab"`
	
	// Relationships
	Course     *Course     `gorm:"foreignKey:CourseID" json:"course,omitempty"`
	Staff      []Staff     `gorm:"many2many:staff_modules;" json:"staff,omitempty"`
	Timetables []Timetable `json:"timetables,omitempty"`
}