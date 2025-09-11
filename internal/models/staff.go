package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Staff struct {
	ID          uint              `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	DeletedAt   gorm.DeletedAt    `gorm:"index" json:"-"`
	Name        string            `gorm:"not null" json:"name" validate:"required,min=2,max=100"`
	Email       string            `gorm:"unique;not null" json:"email" validate:"required,email"`
	FacultyID   uint              `gorm:"not null" json:"faculty_id" validate:"required"`
	Preferences datatypes.JSON    `json:"preferences"` // JSON field for flexible preferences
	MaxHours    int               `gorm:"default:40" json:"max_hours" validate:"min=1,max=60"`
	
	// Relationships
	Faculty    Faculty     `gorm:"foreignKey:FacultyID" json:"faculty,omitempty"`
	Modules    []Module    `gorm:"many2many:staff_modules;" json:"modules,omitempty"`
	Timetables []Timetable `json:"timetables,omitempty"`
}

// StaffPreferences represents the structure for staff preferences JSON
type StaffPreferences struct {
	DayOffs           []string `json:"day_offs"`           // e.g., ["monday", "friday"]
	UnavailableSlots  []string `json:"unavailable_slots"`  // e.g., ["08:00-09:00", "17:00-18:00"]
	PreferredTimes    []string `json:"preferred_times"`    // e.g., ["morning", "afternoon"]
	MaxConsecutive    int      `json:"max_consecutive"`    // Max consecutive teaching hours
	TravelBuffer      int      `json:"travel_buffer"`      // Minutes between classes for travel
}