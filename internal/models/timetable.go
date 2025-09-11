package models

import (
	"time"

	"gorm.io/gorm"
)

type Weekday string

const (
	Monday    Weekday = "monday"
	Tuesday   Weekday = "tuesday"
	Wednesday Weekday = "wednesday"
	Thursday  Weekday = "thursday"
	Friday    Weekday = "friday"
	Saturday  Weekday = "saturday"
	Sunday    Weekday = "sunday"
)

// IsValid checks if the weekday is valid
func (w Weekday) IsValid() bool {
	return w == Monday || w == Tuesday || w == Wednesday || w == Thursday || w == Friday || w == Saturday || w == Sunday
}

type Timetable struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	
	// Foreign Keys
	ClassID   uint  `gorm:"not null" json:"class_id" validate:"required"`
	ModuleID  *uint `json:"module_id"`  // Nullable for general subjects
	SubjectID *uint `json:"subject_id"` // Nullable for course modules
	StaffID   uint  `gorm:"not null" json:"staff_id" validate:"required"`
	RoomID    uint  `gorm:"not null" json:"room_id" validate:"required"`
	
	// Schedule
	Day       Weekday `gorm:"not null" json:"day" validate:"required"`
	StartTime string  `gorm:"not null" json:"start_time" validate:"required"` // Format: "HH:MM"
	EndTime   string  `gorm:"not null" json:"end_time" validate:"required"`   // Format: "HH:MM"
	
	// Relationships
	Class   Class    `gorm:"foreignKey:ClassID" json:"class,omitempty"`
	Module  *Module  `gorm:"foreignKey:ModuleID" json:"module,omitempty"`
	Subject *Subject `gorm:"foreignKey:SubjectID" json:"subject,omitempty"`
	Staff   Staff    `gorm:"foreignKey:StaffID" json:"staff,omitempty"`
	Room    Room     `gorm:"foreignKey:RoomID" json:"room,omitempty"`
}

// GetDuration returns the duration of the timetable entry in minutes
func (t *Timetable) GetDuration() int {
	// Parse start and end times and calculate duration
	// This is a simplified implementation - you might want to use time parsing
	return 60 // Default 1 hour, implement proper time parsing as needed
}

// IsValidTimeSlot checks if the time slot is valid (start < end)
func (t *Timetable) IsValidTimeSlot() bool {
	return t.StartTime < t.EndTime
}