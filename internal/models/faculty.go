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
	
	// Relationships
	Courses []Course `json:"courses,omitempty"`
	Staff   []Staff  `json:"staff,omitempty"`
}