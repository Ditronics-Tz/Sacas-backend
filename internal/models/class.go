package models

import (
	"time"

	"gorm.io/gorm"
)

type Class struct {
	ID               uint           `gorm:"primaryKey" json:"id"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
	Name             string         `gorm:"not null" json:"name" validate:"required,min=1,max=100"`
	CourseID         uint           `gorm:"not null" json:"course_id" validate:"required"`
	Year             int            `gorm:"not null" json:"year" validate:"required,min=1,max=6"` // year of study 1–6
	AcademicYear     string         `json:"academic_year"`                                         // e.g. "2024/25"
	NumberOfStudents int            `gorm:"not null" json:"number_of_students" validate:"required,min=1"`

	// Relationships
	Course     Course      `gorm:"foreignKey:CourseID" json:"course,omitempty"`
	Timetables []Timetable `json:"timetables,omitempty"`
}
