package repositories

import (
	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

type RoomRepository interface {
	Create(room *models.Room) error
	GetByID(id uint) (*models.Room, error)
	Update(room *models.Room) error
	Delete(id uint) error
	GetAll(limit, offset int) ([]models.Room, error)
	GetByCapacity(minCapacity int) ([]models.Room, error)
	GetLabRooms() ([]models.Room, error)
	GetStickyRooms() ([]models.Room, error)
	GetAvailableRooms(day models.Weekday, startTime, endTime string) ([]models.Room, error)
}

type roomRepository struct {
	db *gorm.DB
}

func NewRoomRepository(db *gorm.DB) RoomRepository {
	return &roomRepository{db: db}
}

func (r *roomRepository) Create(room *models.Room) error {
	return r.db.Create(room).Error
}

func (r *roomRepository) GetByID(id uint) (*models.Room, error) {
	var room models.Room
	err := r.db.First(&room, id).Error
	if err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *roomRepository) Update(room *models.Room) error {
	return r.db.Save(room).Error
}

func (r *roomRepository) Delete(id uint) error {
	return r.db.Delete(&models.Room{}, id).Error
}

func (r *roomRepository) GetAll(limit, offset int) ([]models.Room, error) {
	var rooms []models.Room
	err := r.db.Limit(limit).Offset(offset).Find(&rooms).Error
	return rooms, err
}

func (r *roomRepository) GetByCapacity(minCapacity int) ([]models.Room, error) {
	var rooms []models.Room
	err := r.db.Where("capacity >= ?", minCapacity).Find(&rooms).Error
	return rooms, err
}

func (r *roomRepository) GetLabRooms() ([]models.Room, error) {
	var rooms []models.Room
	err := r.db.Where("features::jsonb @> '{\"lab\": true}'").Find(&rooms).Error
	return rooms, err
}

func (r *roomRepository) GetStickyRooms() ([]models.Room, error) {
	var rooms []models.Room
	err := r.db.Where("sticky = ?", true).Find(&rooms).Error
	return rooms, err
}

func (r *roomRepository) GetAvailableRooms(day models.Weekday, startTime, endTime string) ([]models.Room, error) {
	var rooms []models.Room
	
	// Get rooms that don't have conflicting timetable entries
	subQuery := r.db.Model(&models.Timetable{}).
		Select("room_id").
		Where("day = ?", day).
		Where("(start_time < ? AND end_time > ?) OR (start_time < ? AND end_time > ?) OR (start_time >= ? AND end_time <= ?)",
			endTime, startTime, startTime, endTime, startTime, endTime)
	
	err := r.db.Where("id NOT IN (?)", subQuery).Find(&rooms).Error
	return rooms, err
}