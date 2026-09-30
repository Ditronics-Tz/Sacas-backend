package repositories

import (
	"time"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// ExamRepository is tenant-scoped: every method takes an institutionID first.
// Pass PlatformScope (0) only for a platform super_admin.
type ExamRepository interface {
	Create(institutionID uint, exam *models.Exam) error
	GetByID(institutionID, id uint) (*models.Exam, error)
	Update(institutionID uint, exam *models.Exam) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.Exam, error)
	GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Exam, error)
	GetByStatus(institutionID uint, status models.ExamStatus, limit, offset int) ([]models.Exam, error)
	GetByDateRange(institutionID uint, from, to string, limit, offset int) ([]models.Exam, error)
	// SetStatus moves an exam through its lifecycle and stamps the
	// publish/approve timestamps on the transitions that have them.
	SetStatus(institutionID, id uint, status models.ExamStatus, userID uint, at time.Time) (bool, error)
	// CheckRoomConflicts finds exams that overlap the given slot in a room. Both
	// sides are tenant-scoped, so a busy room at another institution never
	// blocks a slot here.
	CheckRoomConflicts(institutionID, roomID uint, date, startTime, endTime string, excludeID uint) ([]models.Exam, error)
	// CheckInvigilatorConflicts finds overlapping exams for one invigilator.
	CheckInvigilatorConflicts(institutionID, invigilatorID uint, date, startTime, endTime string, excludeID uint) ([]models.Exam, error)
}

type examRepository struct {
	db *gorm.DB
}

func NewExamRepository(db *gorm.DB) ExamRepository {
	return &examRepository{db: db}
}

func (r *examRepository) Create(institutionID uint, exam *models.Exam) error {
	if exam == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		exam.InstitutionID = institutionID
	}
	if exam.Status == "" {
		exam.Status = models.ExamStatusDraft
	}
	return r.db.Create(exam).Error
}

func (r *examRepository) GetByID(institutionID, id uint) (*models.Exam, error) {
	var exam models.Exam
	err := TenantScope(r.db, institutionID).First(&exam, id).Error
	if err != nil {
		return nil, err
	}
	return &exam, nil
}

func (r *examRepository) Update(institutionID uint, exam *models.Exam) error {
	if exam == nil {
		return gorm.ErrInvalidValue
	}
	res := TenantScope(r.db.Model(&models.Exam{}), institutionID).
		Where("id = ?", exam.ID).
		Updates(map[string]any{
			"title":          exam.Title,
			"type":           exam.Type,
			"course_id":      exam.CourseID,
			"module_id":      exam.ModuleID,
			"class_ids":      exam.ClassIDs,
			"exam_date":      exam.ExamDate,
			"start_time":     exam.StartTime,
			"end_time":       exam.EndTime,
			"room_id":        exam.RoomID,
			"invigilator_id": exam.InvigilatorID,
			"max_marks":      exam.MaxMarks,
			"notes":          exam.Notes,
			"updated_at":     gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *examRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Exam{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *examRepository) GetAll(institutionID uint, limit, offset int) ([]models.Exam, error) {
	var exams []models.Exam
	err := TenantScope(r.db, institutionID).
		Order("exam_date DESC, id DESC").
		Limit(limit).Offset(offset).
		Find(&exams).Error
	return exams, err
}

func (r *examRepository) GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Exam, error) {
	var exams []models.Exam
	err := TenantScope(r.db, institutionID).
		Where("course_id = ?", courseID).
		Order("exam_date DESC, id DESC").
		Limit(limit).Offset(offset).
		Find(&exams).Error
	return exams, err
}

func (r *examRepository) GetByStatus(institutionID uint, status models.ExamStatus, limit, offset int) ([]models.Exam, error) {
	var exams []models.Exam
	err := TenantScope(r.db, institutionID).
		Where("status = ?", status).
		Order("exam_date DESC, id DESC").
		Limit(limit).Offset(offset).
		Find(&exams).Error
	return exams, err
}

func (r *examRepository) GetByDateRange(institutionID uint, from, to string, limit, offset int) ([]models.Exam, error) {
	var exams []models.Exam
	err := TenantScope(r.db, institutionID).
		Where("exam_date BETWEEN ? AND ?", from, to).
		Order("exam_date DESC, id DESC").
		Limit(limit).Offset(offset).
		Find(&exams).Error
	return exams, err
}

// SetStatus moves an exam through the lifecycle, stamping the publish and
// approve timestamps on the transitions that have them.
//
// The status is written with a WHERE clause on the CURRENT status, so two
// concurrent transitions cannot both succeed: the second finds the row already
// moved and affects no rows.
func (r *examRepository) SetStatus(institutionID, id uint, status models.ExamStatus, userID uint, at time.Time) (bool, error) {
	var exam models.Exam
	if err := TenantScope(r.db, institutionID).First(&exam, id).Error; err != nil {
		return false, err
	}
	if !exam.Status.CanTransitionTo(status) {
		// Not an error: an illegal transition is a caller mistake the controller
		// reports as 409, and reporting it as a row-not-found would hide the
		// real reason.
		return false, ErrInvalidExamTransition
	}

	updates := map[string]any{"status": status, "updated_at": at}
	switch status {
	case models.ExamStatusPublished:
		updates["published_at"] = at
		updates["published_by_id"] = userID
	case models.ExamStatusApproved:
		updates["approved_at"] = at
		updates["approved_by_id"] = userID
	}

	// Optimistic concurrency: only apply if the status is still what we read.
	res := TenantScope(r.db.Model(&models.Exam{}), institutionID).
		Where("id = ? AND status = ?", id, exam.Status).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ErrInvalidExamTransition is returned when a lifecycle change is not allowed.
var ErrInvalidExamTransition = gorm.ErrInvalidValue

// overlapQuery builds the shared "overlaps this window" predicate.
func overlapQuery(query *gorm.DB, date, startTime, endTime string) *gorm.DB {
	return query.
		Where("exam_date = ?", date).
		Where("(start_time < ? AND end_time > ?) OR (start_time < ? AND end_time > ?) OR (start_time >= ? AND end_time <= ?)",
			endTime, startTime, startTime, endTime, startTime, endTime)
}

func (r *examRepository) CheckRoomConflicts(institutionID, roomID uint, date, startTime, endTime string, excludeID uint) ([]models.Exam, error) {
	query := TenantScope(r.db.Model(&models.Exam{}), institutionID).
		Where("room_id = ?", roomID)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var conflicts []models.Exam
	err := overlapQuery(query, date, startTime, endTime).Find(&conflicts).Error
	return conflicts, err
}

func (r *examRepository) CheckInvigilatorConflicts(institutionID, invigilatorID uint, date, startTime, endTime string, excludeID uint) ([]models.Exam, error) {
	query := TenantScope(r.db.Model(&models.Exam{}), institutionID).
		Where("invigilator_id = ?", invigilatorID)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var conflicts []models.Exam
	err := overlapQuery(query, date, startTime, endTime).Find(&conflicts).Error
	return conflicts, err
}
