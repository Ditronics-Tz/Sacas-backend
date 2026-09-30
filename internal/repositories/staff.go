package repositories

import (
	"fmt"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// StaffRepository is tenant-scoped: every method takes an institutionID as
// its first argument. Pass PlatformScope (0) only for a platform super_admin.
//
// Staff email is unique per institution, not globally, so every email lookup
// requires an institutionID.
type StaffRepository interface {
	Create(institutionID uint, staff *models.Staff) error
	GetByID(institutionID, id uint) (*models.Staff, error)
	GetByEmail(institutionID uint, email string) (*models.Staff, error)
	GetByUserID(institutionID, userID uint) (*models.Staff, error)
	Update(institutionID uint, staff *models.Staff) error
	Delete(institutionID, id uint) error
	GetAll(institutionID uint, limit, offset int) ([]models.Staff, error)
	GetByFaculty(institutionID, facultyID uint, limit, offset int) ([]models.Staff, error)
	GetWithModules(institutionID, id uint) (*models.Staff, error)
	UpdatePreferences(institutionID, id uint, preferences string) error
	// SetUserLink changes the login↔staff link. It is separate from Update
	// because the link is a privileged field: a plain profile update must not
	// be able to re-point a staff record at a different account.
	SetUserLink(institutionID, staffID uint, userID *uint) error
	AssignModule(institutionID, staffID, moduleID uint) error
	UnassignModule(institutionID, staffID, moduleID uint) error
	ListModules(institutionID, staffID uint) ([]models.Module, error)
	ListStaffForModule(institutionID, moduleID uint) ([]models.Staff, error)
}

type staffRepository struct {
	db *gorm.DB
}

func NewStaffRepository(db *gorm.DB) StaffRepository {
	return &staffRepository{db: db}
}

func (r *staffRepository) Create(institutionID uint, staff *models.Staff) error {
	if staff == nil {
		return gorm.ErrInvalidValue
	}
	if !IsPlatformScope(institutionID) {
		staff.InstitutionID = institutionID
	}
	return r.db.Create(staff).Error
}

func (r *staffRepository) GetByID(institutionID, id uint) (*models.Staff, error) {
	var staff models.Staff
	err := TenantScope(r.db, institutionID).Preload("Faculty").First(&staff, id).Error
	if err != nil {
		return nil, err
	}
	return &staff, nil
}

func (r *staffRepository) GetByEmail(institutionID uint, email string) (*models.Staff, error) {
	var staff models.Staff
	err := TenantScope(r.db, institutionID).
		Preload("Faculty").
		Where("email = ?", email).
		First(&staff).Error
	if err != nil {
		return nil, err
	}
	return &staff, nil
}

// GetByUserID resolves the Staff record linked to a login (User) account.
// This is the trusted path used by /protected/timetable/my — it never relies
// on any client-supplied staff identifier. The tenant scope is applied as well,
// so a user cannot resolve a staff record belonging to another institution.
func (r *staffRepository) GetByUserID(institutionID, userID uint) (*models.Staff, error) {
	var staff models.Staff
	err := TenantScope(r.db, institutionID).
		Preload("Faculty").
		Where("user_id = ?", userID).
		First(&staff).Error
	if err != nil {
		return nil, err
	}
	return &staff, nil
}

func (r *staffRepository) Update(institutionID uint, staff *models.Staff) error {
	if staff == nil {
		return gorm.ErrInvalidValue
	}
	// institution_id and user_id are deliberately excluded: a record must not
	// be moved between tenants, nor have its login link swapped, by a plain
	// update. Linking goes through the dedicated admin endpoint.
	res := TenantScope(r.db.Model(&models.Staff{}), institutionID).
		Where("id = ?", staff.ID).
		Updates(map[string]any{
			"name":         staff.Name,
			"email":        staff.Email,
			"faculty_id":   staff.FacultyID,
			"preferences":  staff.Preferences,
			"max_hours":    staff.MaxHours,
			"rfid_id":      staff.RfidID,
			"phone_number": staff.PhoneNumber,
			"title":        staff.Title,
			"staff_type":   staff.StaffType,
			"updated_at":   gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *staffRepository) Delete(institutionID, id uint) error {
	res := TenantScope(r.db, institutionID).Delete(&models.Staff{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *staffRepository) GetAll(institutionID uint, limit, offset int) ([]models.Staff, error) {
	var staff []models.Staff
	err := TenantScope(r.db, institutionID).Preload("Faculty").Limit(limit).Offset(offset).Find(&staff).Error
	return staff, err
}

func (r *staffRepository) GetByFaculty(institutionID, facultyID uint, limit, offset int) ([]models.Staff, error) {
	var staff []models.Staff
	err := TenantScope(r.db, institutionID).
		Preload("Faculty").
		Where("faculty_id = ?", facultyID).
		Limit(limit).Offset(offset).
		Find(&staff).Error
	return staff, err
}

func (r *staffRepository) GetWithModules(institutionID, id uint) (*models.Staff, error) {
	var staff models.Staff
	err := TenantScope(r.db, institutionID).
		Preload("Faculty").
		Preload("Modules").
		First(&staff, id).Error
	if err != nil {
		return nil, err
	}
	return &staff, nil
}

func (r *staffRepository) UpdatePreferences(institutionID, id uint, preferences string) error {
	res := TenantScope(r.db.Model(&models.Staff{}), institutionID).
		Where("id = ?", id).
		Updates(map[string]any{
			"preferences": preferences,
			"updated_at":  gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SetUserLink points a staff record at a login account, or clears the link
// when userID is nil. A NULL user_id is written as SQL NULL rather than 0, so
// the partial unique index on user_id is not tripped by an unlinked row.
func (r *staffRepository) SetUserLink(institutionID, staffID uint, userID *uint) error {
	q := TenantScope(r.db.Model(&models.Staff{}), institutionID).Where("id = ?", staffID)
	if userID == nil {
		q = q.Update("user_id", nil)
	} else {
		q = q.Update("user_id", *userID)
	}
	res := q
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// AssignModule links a staff member to a module.
//
// Both the staff row and the module row are resolved inside the institution
// first, so an assignment can never pair a staff member with a module from
// another tenant. The join row is stamped with the institution as well.
func (r *staffRepository) AssignModule(institutionID, staffID, moduleID uint) error {
	if IsPlatformScope(institutionID) {
		// Resolve the tenant from the staff row so the join row is still
		// stamped correctly when a platform admin performs the assignment.
		var staff models.Staff
		if err := r.db.First(&staff, staffID).Error; err != nil {
			return err
		}
		institutionID = staff.InstitutionID
	}
	staff, err := r.GetByID(institutionID, staffID)
	if err != nil {
		return err
	}
	if _, err := moduleExistsInInstitution(r.db, institutionID, moduleID); err != nil {
		return err
	}
	return r.db.Exec(
		`INSERT INTO staff_modules (staff_id, module_id, institution_id)
		 VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
		staffID, moduleID, staff.InstitutionID,
	).Error
}

func (r *staffRepository) UnassignModule(institutionID, staffID, moduleID uint) error {
	if _, err := r.GetByID(institutionID, staffID); err != nil {
		return err
	}
	return r.db.Exec(
		`DELETE FROM staff_modules WHERE staff_id = ? AND module_id = ? AND institution_id = ?`,
		staffID, moduleID, institutionID,
	).Error
}

func (r *staffRepository) ListModules(institutionID, staffID uint) ([]models.Module, error) {
	if _, err := r.GetByID(institutionID, staffID); err != nil {
		return nil, err
	}
	var modules []models.Module
	// The join table carries institution_id, so the link cannot point at a
	// module from another tenant even if the IDs were tampered with.
	err := TenantScope(r.db, institutionID).
		Joins("JOIN staff_modules ON staff_modules.module_id = modules.id AND staff_modules.staff_id = ? AND staff_modules.institution_id = ?", staffID, institutionID).
		Find(&modules).Error
	return modules, err
}

func (r *staffRepository) ListStaffForModule(institutionID, moduleID uint) ([]models.Staff, error) {
	if _, err := moduleExistsInInstitution(r.db, institutionID, moduleID); err != nil {
		return nil, err
	}
	var staff []models.Staff
	err := TenantScope(r.db, institutionID).
		Joins("JOIN staff_modules ON staff_modules.staff_id = staffs.id AND staff_modules.module_id = ? AND staff_modules.institution_id = ?", moduleID, institutionID).
		Find(&staff).Error
	return staff, err
}

// moduleExistsInInstitution returns gorm.ErrRecordNotFound when the module is
// missing or owned by another institution.
func moduleExistsInInstitution(db *gorm.DB, institutionID, moduleID uint) (*models.Module, error) {
	var module models.Module
	if err := TenantScope(db, institutionID).First(&module, moduleID).Error; err != nil {
		return nil, fmt.Errorf("module %d: %w", moduleID, err)
	}
	return &module, nil
}
