package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

type TimetableRepository interface {
	Create(timetable *models.Timetable) error
	GetByID(id uint) (*models.Timetable, error)
	Update(timetable *models.Timetable) error
	Delete(id uint) error
	DeleteByClass(classID uint) error
	ReplaceClassTimetable(classID uint, entries []models.Timetable) ([]models.Timetable, error)
	GetAll(limit, offset int) ([]models.Timetable, error)
	GetByClass(classID uint) ([]models.Timetable, error)
	GetByStaff(staffID uint) ([]models.Timetable, error)
	GetByRoom(roomID uint) ([]models.Timetable, error)
	GetByDay(day models.Weekday) ([]models.Timetable, error)
	// CheckConflicts finds overlapping bookings. Pass excludeID > 0 to ignore a row (updates).
	CheckConflicts(classID, staffID, roomID uint, day models.Weekday, startTime, endTime string, excludeID uint) ([]models.Timetable, error)
	GetByDateRange(startDate, endDate string) ([]models.Timetable, error)
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

func (r *timetableRepository) Create(timetable *models.Timetable) error {
	return r.db.Create(timetable).Error
}

func (r *timetableRepository) GetByID(id uint) (*models.Timetable, error) {
	var timetable models.Timetable
	err := r.db.Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room").First(&timetable, id).Error
	if err != nil {
		return nil, err
	}
	return &timetable, nil
}

func (r *timetableRepository) Update(timetable *models.Timetable) error {
	return r.db.Save(timetable).Error
}

func (r *timetableRepository) Delete(id uint) error {
	return r.db.Delete(&models.Timetable{}, id).Error
}

func (r *timetableRepository) DeleteByClass(classID uint) error {
	// Hard-delete to avoid soft-delete tombstone growth on regenerate
	return r.db.Unscoped().Where("class_id = ?", classID).Delete(&models.Timetable{}).Error
}

// ReplaceClassTimetable hard-deletes existing class slots and inserts new ones in one transaction.
func (r *timetableRepository) ReplaceClassTimetable(classID uint, entries []models.Timetable) ([]models.Timetable, error) {
	var out []models.Timetable
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// Unscoped: permanent delete so regenerations do not accumulate tombstones
		if err := tx.Unscoped().Where("class_id = ?", classID).Delete(&models.Timetable{}).Error; err != nil {
			return err
		}
		for i := range entries {
			entries[i].ID = 0
			entries[i].ClassID = classID
			if err := tx.Create(&entries[i]).Error; err != nil {
				return err
			}
			var full models.Timetable
			if err := tx.Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room").
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

func (r *timetableRepository) GetAll(limit, offset int) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.db.Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room").
		Limit(limit).Offset(offset).Find(&timetables).Error
	return timetables, err
}

func (r *timetableRepository) GetByClass(classID uint) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.db.Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room").
		Where("class_id = ?", classID).Find(&timetables).Error
	return timetables, err
}

func (r *timetableRepository) GetByStaff(staffID uint) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.db.Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room").
		Where("staff_id = ?", staffID).Find(&timetables).Error
	return timetables, err
}

func (r *timetableRepository) GetByRoom(roomID uint) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.db.Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room").
		Where("room_id = ?", roomID).Find(&timetables).Error
	return timetables, err
}

func (r *timetableRepository) GetByDay(day models.Weekday) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.db.Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room").
		Where("day = ?", day).Find(&timetables).Error
	return timetables, err
}

func (r *timetableRepository) CheckConflicts(classID, staffID, roomID uint, day models.Weekday, startTime, endTime string, excludeID uint) ([]models.Timetable, error) {
	var conflicts []models.Timetable

	query := r.db.Where("day = ?", day).Where(
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

func (r *timetableRepository) GetByDateRange(startDate, endDate string) ([]models.Timetable, error) {
	var timetables []models.Timetable
	err := r.db.Preload("Class").Preload("Module").Preload("Subject").Preload("Staff").Preload("Room").
		Where("created_at BETWEEN ? AND ?", startDate, endDate).Find(&timetables).Error
	return timetables, err
}
