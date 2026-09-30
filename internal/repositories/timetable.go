package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// TimetableRepository is tenant-scoped: every method takes an institutionID as
// its first argument. Pass PlatformScope (0) only for a platform super_admin.
type TimetableRepository interface {
	Create(institutionID uint, timetable *models.Timetable) error
	GetByID(institutionID, id uint) (*models.Timetable, error)
	Update(institutionID uint, timetable *models.Timetable) error
	Delete(institutionID, id uint) error
	DeleteByClass(institutionID, classID uint) error
	ReplaceClassTimetable(institutionID, classID uint, entries []models.Timetable) ([]models.Timetable, error)
	GetAll(institutionID uint, limit, offset int) ([]models.Timetable, error)
	GetByClass(institutionID, classID uint) ([]models.Timetable, error)
	GetByStaff(institutionID, staffID uint) ([]models.Timetable, error)
	// GetByCourse returns timetable entries for all classes belonging to a
	// course (course → classes → timetable entries) in a single query.
	// A course with no classes/entries yields an empty (non-nil) slice.
	GetByCourse(institutionID, courseID uint) ([]models.Timetable, error)
	GetByRoom(institutionID, roomID uint) ([]models.Timetable, error)
	GetByDay(institutionID uint, day models.Weekday) ([]models.Timetable, error)
	// CheckConflicts finds overlapping bookings. Pass excludeID > 0 to ignore a row (updates).
	CheckConflicts(institutionID, classID, staffID, roomID uint, day models.Weekday, startTime, endTime string, excludeID uint) ([]models.Timetable, error)
	GetByDateRange(institutionID uint, startDate, endDate string) ([]models.Timetable, error)
	DB() *gorm.DB
}

type timetableRepository struct {
	db *gorm.DB
}

func NewTimetableRepository(db *gorm.DB) TimetableRepository {
	return &timetableRepository{db: db}
}

func (r *timetableRepository) DB() *gorm.DB {
	return r.db
}

// withAssociations applies the tenant scope plus the standard preloads.
func (r *timetableRepository) withAssociations(institutionID uint) *gorm.DB {
	return TenantScope(r.db, institutionID).
		Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room")
}

func (r *timetableRepository) Create(institutionID uint, timetable *models.Timetable) error {
	if timetable == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		timetable.InstitutionID = institutionID
	}
	return r.db.Create(timetable).Error
}

func (r *timetableRepository) GetByID(institutionID, id uint) (*models.Timetable, error) {
	var timetable models.Timetable
	err := r.withAssociations(institutionID).First(&timetable, id).Error
	if err != nil {
		return nil, err
	}
	return &timetable, nil
}

func (r *timetableRepository) Update(institutionID uint, timetable *models.Timetable) error {
	if timetable == nil {
		return gorm.ErrInvalidValue
	}
	res := TenantScope(r.db.Model(&models.Timetable{}), institutionID).
		Where("id = ?", timetable.ID).
		Updates(map[string]any{
			"class_id":   timetable.ClassID,
			"module_id":  timetable.ModuleID,
			"subject_id": timetable.SubjectID,
			"staff_id":   timetable.StaffID,
			"room_id":    timetable.RoomID,
			"day":        timetable.Day,
			"start_time": timetable.StartTime,
			"end_time":   timetable.EndTime,
			"updated_at": gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *timetableRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Timetable{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *timetableRepository) DeleteByClass(institutionID, classID uint) error {
	// Hard-delete to avoid soft-delete tombstone growth on regenerate.
	// The tenant scope is applied BEFORE Unscoped() so a regenerate can only
	// ever wipe the caller's own rows.
	return TenantScope(r.db, institutionID).
		Unscoped().
		Where("class_id = ?", classID).
		Delete(&models.Timetable{}).Error
}

// ReplaceClassTimetable hard-deletes existing class slots and inserts new ones
// in one transaction. The class must belong to the institution, and every new
// entry is stamped with that institution.
func (r *timetableRepository) ReplaceClassTimetable(institutionID, classID uint, entries []models.Timetable) ([]models.Timetable, error) {
	var out []models.Timetable
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// Confirm the class is in the tenant before touching any rows, so a
		// foreign class ID is a no-op rather than a silent cross-tenant wipe.
		var class models.Class
		if err := TenantScope(tx, institutionID).First(&class, classID).Error; err != nil {
			return err
		}
		// Unscoped: permanent delete so regenerations do not accumulate tombstones
		if err := TenantScope(tx, institutionID).
			Unscoped().
			Where("class_id = ?", classID).
			Delete(&models.Timetable{}).Error; err != nil {
			return err
		}
		for i := range entries {
			entries[i].ID = 0
			entries[i].ClassID = classID
			if !IsPlatformScope(institutionID) {
				entries[i].InstitutionID = institutionID
			}
			if err := tx.Create(&entries[i]).Error; err != nil {
				return err
			}
			var full models.Timetable
			if err := TenantScope(tx, institutionID).
				Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room").
				First(&full, entries[i].ID).Error; err != nil {
				out = append(out, entries[i])
			} else {
				out = append(out, full)
			}
		}
		return nil
	})
	return out, err
}

func (r *timetableRepository) GetAll(institutionID uint, limit, offset int) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.withAssociations(institutionID).Limit(limit).Offset(offset).Find(&timetables).Error
	return timetables, err
}

func (r *timetableRepository) GetByClass(institutionID, classID uint) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.withAssociations(institutionID).Where("class_id = ?", classID).Find(&timetables).Error
	return timetables, err
}

func (r *timetableRepository) GetByStaff(institutionID, staffID uint) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.withAssociations(institutionID).Where("staff_id = ?", staffID).Find(&timetables).Error
	return timetables, err
}

func (r *timetableRepository) GetByCourse(institutionID, courseID uint) ([]models.Timetable, error) {
	// Join through classes via a subquery (house style — no raw JOINs):
	// timetables WHERE class_id IN (SELECT id FROM classes WHERE course_id = ?).
	// The subquery is tenant-scoped too, so a course from another institution
	// resolves to an empty result rather than leaking its classes.
	classesSubQuery := TenantScope(r.db.Model(&models.Class{}), institutionID).
		Select("id").
		Where("course_id = ?", courseID)

	var timetables []models.Timetable
	err := r.withAssociations(institutionID).
		Where("class_id IN (?)", classesSubQuery).
		Find(&timetables).Error
	if timetables == nil {
		// Course with no classes/entries is an empty result, not an error;
		// return [] so JSON marshals as [] rather than null.
		timetables = []models.Timetable{}
	}
	return timetables, err
}

func (r *timetableRepository) GetByRoom(institutionID, roomID uint) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.withAssociations(institutionID).Where("room_id = ?", roomID).Find(&timetables).Error
	return timetables, err
}

func (r *timetableRepository) GetByDay(institutionID uint, day models.Weekday) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.withAssociations(institutionID).Where("day = ?", day).Find(&timetables).Error
	return timetables, err
}

// CheckConflicts finds overlapping bookings within the institution only, so a
// room or staff member being busy at another institution never blocks a slot
// here.
func (r *timetableRepository) CheckConflicts(institutionID, classID, staffID, roomID uint, day models.Weekday, startTime, endTime string, excludeID uint) ([]models.Timetable, error) {
	var conflicts []models.Timetable

	query := TenantScope(r.db, institutionID).
		Where("day = ?", day).
		Where(
			"(start_time < ? AND end_time > ?) OR (start_time < ? AND end_time > ?) OR (start_time >= ? AND end_time <= ?)",
			endTime, startTime, startTime, endTime, startTime, endTime,
		)
	query = query.Where("class_id = ? OR staff_id = ? OR room_id = ?", classID, staffID, roomID)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}

	err := query.Find(&conflicts).Error
	return conflicts, err
}

func (r *timetableRepository) GetByDateRange(institutionID uint, startDate, endDate string) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.withAssociations(institutionID).
		Where("created_at BETWEEN ? AND ?", startDate, endDate).
		Find(&timetables).Error
	return timetables, err
}
