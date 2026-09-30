package models

import (
	"time"

	"gorm.io/gorm"
)

// ExamType distinguishes the kinds of assessment an institution runs.
type ExamType string

const (
	ExamTypeMidterm    ExamType = "midterm"
	ExamTypeFinal      ExamType = "final"
	ExamTypeQuiz       ExamType = "quiz"
	ExamTypePractical  ExamType = "practical"
	ExamTypeSupplement ExamType = "supplement"
)

func (t ExamType) IsValid() bool {
	switch t {
	case ExamTypeMidterm, ExamTypeFinal, ExamTypeQuiz, ExamTypePractical, ExamTypeSupplement:
		return true
	}
	return false
}

// ExamStatus is the lifecycle of an exam paper.
//
//	draft      → being written
//	scheduled  → a date, time, room, and invigilator are set
//	published  → students can see it
//	approved   → signed off; results may be entered
//
// Publish and approve are separate states, mirroring the timetable lifecycle, so
// the person who sets the paper is not automatically the person who signs it
// off.
type ExamStatus string

const (
	ExamStatusDraft     ExamStatus = "draft"
	ExamStatusScheduled ExamStatus = "scheduled"
	ExamStatusPublished ExamStatus = "published"
	ExamStatusApproved  ExamStatus = "approved"
)

func (s ExamStatus) IsValid() bool {
	switch s {
	case ExamStatusDraft, ExamStatusScheduled, ExamStatusPublished, ExamStatusApproved:
		return true
	}
	return false
}

// CanTransitionTo reports whether a status change is legal.
//
// Forward movement is strictly one stage at a time: a draft cannot jump straight
// to published or approved, because each stage is a real governance step
// (scheduling sets a room and invigilator, publishing tells students, approving
// authorises results) and skipping one means it did not happen.
//
// Backwards is a single WITHDRAW to draft, from either scheduled or published.
// That is one deliberate action — "take this back off the board" — rather than a
// stage-by-stage rewind, which would otherwise require two meaningless
// intermediate states an operator would never intend.
//
// Approved is terminal. Undoing a sign-off is a separate, deliberate action, not
// a status edit.
func (s ExamStatus) CanTransitionTo(next ExamStatus) bool {
	order := map[ExamStatus]int{
		ExamStatusDraft:     0,
		ExamStatusScheduled: 1,
		ExamStatusPublished: 2,
		ExamStatusApproved:  3,
	}
	from, ok := order[s]
	if !ok {
		return false
	}
	to, ok := order[next]
	if !ok {
		return false
	}
	if s == ExamStatusApproved {
		return false
	}
	// Withdraw.
	if next == ExamStatusDraft && from > 0 {
		return true
	}
	// Advance exactly one stage.
	return to == from+1
}

// Exam is a scheduled assessment, scoped to one institution.
//
// Like every other tenant entity it carries institution_id, and every reference
// on it (course, module, room, invigilator) must belong to the same
// institution — the repository and controller both enforce that, because a
// cross-tenant room or lecturer reference would both leak another campus's data
// and let an exam be scheduled somewhere impossible.
type Exam struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// InstitutionID is the owning tenant.
	InstitutionID uint `gorm:"not null;index" json:"institution_id"`

	Title string   `gorm:"not null" json:"title" validate:"required,min=2,max=150"`
	Type  ExamType `gorm:"not null;default:'final'" json:"type" validate:"required"`
	// CourseID is the course the exam belongs to, which is what makes it
	// discoverable by a coordinator.
	CourseID uint `gorm:"not null;index" json:"course_id" validate:"required"`
	// ModuleID is optional: an exam may cover a whole course.
	ModuleID *uint `gorm:"index" json:"module_id,omitempty"`
	// ClassIDs is the set of classes sitting the exam. Stored as a JSON array
	// because the set is small, read-only once set, and never queried by
	// membership in SQL; a join table would add a write path for no query gain.
	ClassIDs []uint `gorm:"type:jsonb" json:"class_ids"`

	ExamDate  string `gorm:"not null;index" json:"exam_date" validate:"required"` // YYYY-MM-DD
	StartTime string `gorm:"not null" json:"start_time" validate:"required"`      // HH:MM
	EndTime   string `gorm:"not null" json:"end_time" validate:"required"`        // HH:MM

	// RoomID and InvigilatorID (a Staff ID) are set when the exam is scheduled.
	RoomID        *uint `gorm:"index" json:"room_id,omitempty"`
	InvigilatorID *uint `gorm:"index" json:"invigilator_id,omitempty"`

	MaxMarks int        `gorm:"default:100" json:"max_marks"`
	Status   ExamStatus `gorm:"not null;default:'draft';index" json:"status"`
	Notes    string     `json:"notes,omitempty"`

	PublishedAt   *time.Time `json:"published_at,omitempty"`
	PublishedByID *uint      `json:"published_by_id,omitempty"`
	ApprovedAt    *time.Time `json:"approved_at,omitempty"`
	ApprovedByID  *uint      `json:"approved_by_id,omitempty"`
}

// IsScheduled reports whether the exam has the room and invigilator it needs to
// actually happen.
func (e *Exam) IsScheduled() bool {
	return e.RoomID != nil && e.InvigilatorID != nil
}

// HasValidTimeWindow reports whether the exam's times are coherent.
func (e *Exam) HasValidTimeWindow() bool {
	return e.StartTime != "" && e.EndTime != "" && e.StartTime < e.EndTime
}
