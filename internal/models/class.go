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
	AcademicYear     string         `json:"academic_year"`                                        // e.g. "2024/25"
	NumberOfStudents int            `gorm:"not null" json:"number_of_students" validate:"required,min=1"`

	// InstitutionID is the owning tenant. Class names are unique per
	// institution, not globally.
	InstitutionID uint `gorm:"not null;index" json:"institution_id"`

	// --- Timetable lifecycle ---------------------------------------------
	//
	// A generated timetable is a DRAFT until somebody with the publish
	// permission says it is real, and stays UNCONFIRMED until somebody with the
	// approve permission signs it off. Both timestamps are kept separately so
	// the trail can show who generated, who published, and who approved — the
	// common governance requirement is that the person who prepared a schedule
	// does not get to approve it.
	//
	// Regenerating a timetable clears the publish/approval timestamps, because
	// the new draft is a different document from the one that was approved.
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	PublishedByID *uint      `json:"published_by_id,omitempty"`
	ApprovedAt    *time.Time `json:"approved_at,omitempty"`
	ApprovedByID  *uint      `json:"approved_by_id,omitempty"`

	// Relationships
	Institution Institution `gorm:"foreignKey:InstitutionID" json:"institution,omitempty"`
	Course      Course      `gorm:"foreignKey:CourseID" json:"course,omitempty"`
	Timetables  []Timetable `gorm:"foreignKey:ClassID" json:"timetables,omitempty"`
}

// TimetableLifecycle is the derived publication state of a class's timetable.
// Derived rather than stored, so it cannot drift from the timestamps.
type TimetableLifecycle string

const (
	// TimetableEmpty means the class has no timetable entries at all.
	TimetableEmpty TimetableLifecycle = "empty"
	// TimetableDraft means entries exist but nobody has published them.
	TimetableDraft TimetableLifecycle = "draft"
	// TimetablePublished means published but not yet approved.
	TimetablePublished TimetableLifecycle = "published"
	// TimetableApproved means published and approved. This is the only state a
	// student-facing view should show.
	TimetableApproved TimetableLifecycle = "approved"
)

// TimetableState derives the lifecycle from the stored timestamps.
func (c *Class) TimetableState(hasEntries bool) TimetableLifecycle {
	if !hasEntries {
		return TimetableEmpty
	}
	if c.ApprovedAt != nil {
		return TimetableApproved
	}
	if c.PublishedAt != nil {
		return TimetablePublished
	}
	return TimetableDraft
}
