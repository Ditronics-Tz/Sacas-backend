package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// RoomRepository is tenant-scoped: every method takes an institutionID as
// its first argument. Pass PlatformScope (0) only for a platform super_admin.
type RoomRepository interface {
	Create(institutionID uint, room *models.Room) error
	GetByID(institutionID, id uint) (*models.Room, error)
	Update(institutionID uint, room *models.Room) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.Room, error)
	GetByCapacity(institutionID uint, minCapacity int) ([]models.Room, error)
	GetLabRooms(institutionID uint) ([]models.Room, error)
	GetStickyRooms(institutionID uint) ([]models.Room, error)
	GetAvailableRooms(institutionID uint, day models.Weekday, startTime, endTime string) ([]models.Room, error)
}

type roomRepository struct {
	db *gorm.DB
}

func NewRoomRepository(db *gorm.DB) RoomRepository {
	return &roomRepository{db: db}
}

func (r *roomRepository) Create(institutionID uint, room *models.Room) error {
	if room == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		room.InstitutionID = institutionID
	}
	return r.db.Create(room).Error
}

func (r *roomRepository) GetByID(institutionID, id uint) (*models.Room, error) {
	var room models.Room
	err := TenantScope(r.db, institutionID).First(&room, id).Error
	if err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *roomRepository) Update(institutionID uint, room *models.Room) error {
	if room == nil {
		return gorm.ErrInvalidValue
	}
	res := TenantScope(r.db.Model(&models.Room{}), institutionID).
		Where("id = ?", room.ID).
		Updates(map[string]any{
			"name":            room.Name,
			"capacity":        room.Capacity,
			"features":        room.Features,
			"sticky":          room.Sticky,
			"allowed_courses": room.AllowedCourses,
			"updated_at":      gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *roomRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Room{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *roomRepository) GetAll(institutionID uint, limit, offset int) ([]models.Room, error) {
	var rooms []models.Room
	err := TenantScope(r.db, institutionID).Limit(limit).Offset(offset).Find(&rooms).Error
	return rooms, err
}

func (r *roomRepository) GetByCapacity(institutionID uint, minCapacity int) ([]models.Room, error) {
	var rooms []models.Room
	err := TenantScope(r.db, institutionID).Where("capacity >= ?", minCapacity).Find(&rooms).Error
	return rooms, err
}

func (r *roomRepository) GetLabRooms(institutionID uint) ([]models.Room, error) {
	var rooms []models.Room
	err := TenantScope(r.db, institutionID).Where("features::jsonb @> '{\"lab\": true}'").Find(&rooms).Error
	return rooms, err
}

func (r *roomRepository) GetStickyRooms(institutionID uint) ([]models.Room, error) {
	var rooms []models.Room
	err := TenantScope(r.db, institutionID).Where("sticky = ?", true).Find(&rooms).Error
	return rooms, err
}

// GetAvailableRooms returns the institution's rooms with no overlapping entry
// in the given slot. The subquery is scoped to the same institution so a busy
// room at another institution never hides a free one here.
func (r *roomRepository) GetAvailableRooms(institutionID uint, day models.Weekday, startTime, endTime string) ([]models.Room, error) {
	var rooms []models.Room

	subQuery := TenantScope(r.db.Model(&models.Timetable{}), institutionID).
		Select("room_id").
		Where("day = ?", day).
		Where("(start_time < ? AND end_time > ?) OR (start_time < ? AND end_time > ?) OR (start_time >= ? AND end_time <= ?)",
			endTime, startTime, startTime, endTime, startTime, endTime)

	err := TenantScope(r.db, institutionID).Where("id NOT IN (?)", subQuery).Find(&rooms).Error
	return rooms, err
}
