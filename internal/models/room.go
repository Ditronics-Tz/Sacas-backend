package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Room struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
	Name            string         `gorm:"not null" json:"name" validate:"required,min=1,max=100"`
	Capacity        int            `gorm:"not null" json:"capacity" validate:"required,min=1"`
	Features        datatypes.JSON `json:"features"`        // JSON field for room features
	Sticky          bool           `gorm:"default:false" json:"sticky"` // If room is bound to specific modules
	AllowedCourses  datatypes.JSON `json:"allowed_courses"` // JSON field for course restrictions
	
	// Relationships
	Timetables []Timetable `json:"timetables,omitempty"`
}

// RoomFeatures represents the structure for room features JSON
type RoomFeatures struct {
	Projector bool `json:"projector"`
	Lab       bool `json:"lab"`
	Studio    bool `json:"studio"`
	AC        bool `json:"ac"`
	Whiteboard bool `json:"whiteboard"`
	Computers int  `json:"computers"` // Number of computers
}

// AllowedCourses represents course restrictions for rooms
type AllowedCourses struct {
	CourseIDs []uint `json:"course_ids"` // Empty means all courses allowed
}