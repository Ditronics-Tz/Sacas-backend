package models

import (
	"time"

	"gorm.io/gorm"
)

type Subject struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"not null" json:"name" validate:"required,min=2,max=100"`
	CreditHours int            `gorm:"not null" json:"credit_hours" validate:"required,min=1,max=10"`
	
	// Relationships - General subjects can be shared across multiple classes/courses
	Timetables []Timetable `json:"timetables,omitempty"`
}