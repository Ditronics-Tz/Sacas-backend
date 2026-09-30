package repositories

import (
	"time"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// ClassRepository is tenant-scoped: every method takes an institutionID as
// its first argument. Pass PlatformScope (0) only for a platform super_admin.
type ClassRepository interface {
	Create(institutionID uint, class *models.Class) error
	GetByID(institutionID, id uint) (*models.Class, error)
	Update(institutionID uint, class *models.Class) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.Class, error)
	GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Class, error)
	GetByYear(institutionID uint, year int, limit, offset int) ([]models.Class, error)
	// CountTimetableEntries reports how many live timetable rows a class has,
	// which is what distinguishes an empty class from a draft one.
	CountTimetableEntries(institutionID, classID uint) (int64, error)
	// MarkPublished stamps the publish timestamp. It requires the class to have
	// entries, so an empty timetable cannot be published.
	MarkPublished(institutionID, classID, userID uint, at time.Time) (bool, error)
	// MarkApproved stamps the approval timestamp. It requires the timetable to
	// be published first, so approval cannot precede publication.
	MarkApproved(institutionID, classID, userID uint, at time.Time) (bool, error)
	// ClearPublication resets the publish and approval timestamps. Called when a
	// timetable is regenerated, because the new draft is a different document
	// from the one that was approved.
	ClearPublication(institutionID, classID uint) error
}

type classRepository struct {
	db *gorm.DB
}

func NewClassRepository(db *gorm.DB) ClassRepository {
	return &classRepository{db: db}
}

func (r *classRepository) Create(institutionID uint, class *models.Class) error {
	if class == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		class.InstitutionID = institutionID
	}
	return r.db.Create(class).Error
}

func (r *classRepository) GetByID(institutionID, id uint) (*models.Class, error) {
	var class models.Class
	err := TenantScope(r.db, institutionID).Preload("Course").First(&class, id).Error
	if err != nil {
		return nil, err
	}
	return &class, nil
}

func (r *classRepository) Update(institutionID uint, class *models.Class) error {
	if class == nil {
		return gorm.ErrInvalidValue
	}
	res := TenantScope(r.db.Model(&models.Class{}), institutionID).
		Where("id = ?", class.ID).
		Updates(map[string]any{
			"name":               class.Name,
			"course_id":          class.CourseID,
			"year":               class.Year,
			"academic_year":      class.AcademicYear,
			"number_of_students": class.NumberOfStudents,
			"updated_at":         gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *classRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Class{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *classRepository) GetAll(institutionID uint, limit, offset int) ([]models.Class, error) {
	var classes []models.Class
	err := TenantScope(r.db, institutionID).Preload("Course").Limit(limit).Offset(offset).Find(&classes).Error
	return classes, err
}

func (r *classRepository) GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Class, error) {
	var classes []models.Class
	err := TenantScope(r.db, institutionID).
		Preload("Course").
		Where("course_id = ?", courseID).
		Limit(limit).Offset(offset).
		Find(&classes).Error
	return classes, err
}

func (r *classRepository) GetByYear(institutionID uint, year int, limit, offset int) ([]models.Class, error) {
	var classes []models.Class
	err := TenantScope(r.db, institutionID).
		Preload("Course").
		Where("year = ?", year).
		Limit(limit).Offset(offset).
		Find(&classes).Error
	return classes, err
}

// CountTimetableEntries counts live timetable rows for a class.
func (r *classRepository) CountTimetableEntries(institutionID, classID uint) (int64, error) {
	var count int64
	// Both tables are scoped: a class ID from another institution must not even
	// be able to report a count here.
	err := TenantScope(r.db.Model(&models.Timetable{}), institutionID).
		Where("class_id = ?", classID).
		Count(&count).Error
	return count, err
}

// MarkPublished stamps the publish timestamp, but only for a class that has
// entries. Returns false when the class is empty or does not belong to the
// institution, so the caller can distinguish the two by checking first.
func (r *classRepository) MarkPublished(institutionID, classID, userID uint, at time.Time) (bool, error) {
	entries, err := r.CountTimetableEntries(institutionID, classID)
	if err != nil {
		return false, err
	}
	if entries == 0 {
		return false, nil
	}
	res := TenantScope(r.db.Model(&models.Class{}), institutionID).
		Where("id = ?", classID).
		Updates(map[string]any{
			"published_at":    at,
			"published_by_id": userID,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// MarkApproved stamps the approval timestamp. It requires published_at to be set,
// so approval cannot precede publication and the two are always in order in the
// trail.
func (r *classRepository) MarkApproved(institutionID, classID, userID uint, at time.Time) (bool, error) {
	var class models.Class
	if err := TenantScope(r.db, institutionID).First(&class, classID).Error; err != nil {
		return false, err
	}
	if class.PublishedAt == nil {
		return false, nil
	}
	res := TenantScope(r.db.Model(&models.Class{}), institutionID).
		Where("id = ?", classID).
		Updates(map[string]any{
			"approved_at":    at,
			"approved_by_id": userID,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ClearPublication resets the publish and approval timestamps.
func (r *classRepository) ClearPublication(institutionID, classID uint) error {
	return TenantScope(r.db.Model(&models.Class{}), institutionID).
		Where("id = ?", classID).
		Updates(map[string]any{
			"published_at":    nil,
			"published_by_id": nil,
			"approved_at":     nil,
			"approved_by_id":  nil,
		}).Error
}
