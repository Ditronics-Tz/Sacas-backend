package repositories

import (
	"errors"
	"time"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// ErrInvitationNotFound is returned when a token does not resolve to an
// invitation. It is deliberately a single error for "no such token", "already
// accepted", "revoked", and "expired", so an attacker cannot use the error
// message to learn which of those it was.
var ErrInvitationNotFound = errors.New("invitation is not valid")

// InvitationRepository manages institution invitations.
type InvitationRepository interface {
	Create(invitation *models.InstitutionInvitation) error
	// GetByTokenHash resolves a token digest. It does not filter by usability;
	// the caller decides what to do with an unusable invitation, so the audit
	// trail can record why acceptance was refused.
	GetByTokenHash(hash string) (*models.InstitutionInvitation, error)
	GetByID(institutionID, id uint) (*models.InstitutionInvitation, error)
	GetAll(institutionID uint, limit, offset int) ([]models.InstitutionInvitation, error)
	// GetPendingForEmail returns the institution's unaccepted, unrevoked,
	// unexpired invitations for one address, so a repeat invite can reuse the
	// existing row instead of piling up duplicates.
	GetPendingForEmail(institutionID uint, email string, now time.Time) ([]models.InstitutionInvitation, error)
	// MarkAccepted records acceptance. It refuses an invitation that is no
	// longer usable, so a concurrent double-accept cannot both win.
	MarkAccepted(id uint, userID uint, at time.Time) (bool, error)
	Revoke(institutionID, id uint, at time.Time) (bool, error)
	TouchSent(id uint, at time.Time) error
	// CountActiveForInstitution reports how many invitations are outstanding.
	CountActiveForInstitution(institutionID uint, now time.Time) (int64, error)
}

type invitationRepository struct {
	db *gorm.DB
}

func NewInvitationRepository(db *gorm.DB) InvitationRepository {
	return &invitationRepository{db: db}
}

func (r *invitationRepository) Create(invitation *models.InstitutionInvitation) error {
	if invitation == nil {
		return gorm.ErrInvalidValue
	}
	return r.db.Create(invitation).Error
}

func (r *invitationRepository) GetByTokenHash(hash string) (*models.InstitutionInvitation, error) {
	var inv models.InstitutionInvitation
	if err := r.db.Where("token_hash = ?", hash).First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvitationNotFound
		}
		return nil, err
	}
	return &inv, nil
}

func (r *invitationRepository) GetByID(institutionID, id uint) (*models.InstitutionInvitation, error) {
	var inv models.InstitutionInvitation
	// Tenant-scoped: an invitation from another institution is not found.
	if err := TenantScope(r.db, institutionID).First(&inv, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvitationNotFound
		}
		return nil, err
	}
	return &inv, nil
}

func (r *invitationRepository) GetAll(institutionID uint, limit, offset int) ([]models.InstitutionInvitation, error) {
	var invitations []models.InstitutionInvitation
	err := TenantScope(r.db, institutionID).
		Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&invitations).Error
	return invitations, err
}

func (r *invitationRepository) GetPendingForEmail(institutionID uint, email string, now time.Time) ([]models.InstitutionInvitation, error) {
	var invitations []models.InstitutionInvitation
	err := TenantScope(r.db, institutionID).
		Where("email = ?", email).
		Where("accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?", now).
		Order("created_at DESC").
		Find(&invitations).Error
	return invitations, err
}

// MarkAccepted uses a conditional UPDATE so the acceptance is atomic: the WHERE
// clause re-asserts that the invitation is still unused, so two concurrent
// accepts cannot both report success.
func (r *invitationRepository) MarkAccepted(id uint, userID uint, at time.Time) (bool, error) {
	res := r.db.Model(&models.InstitutionInvitation{}).
		Where("id = ?", id).
		Where("accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?", at).
		Updates(map[string]any{
			"accepted_at":      at,
			"accepted_user_id": userID,
			"updated_at":       at,
		})
	if res.Error != nil {
		return false, res.Error
	}
	// Zero rows means someone else won the race, or it was no longer usable.
	return res.RowsAffected > 0, nil
}

func (r *invitationRepository) Revoke(institutionID, id uint, at time.Time) (bool, error) {
	res := TenantScope(r.db.Model(&models.InstitutionInvitation{}), institutionID).
		Where("id = ?", id).
		Where("accepted_at IS NULL").
		Update("revoked_at", at)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *invitationRepository) TouchSent(id uint, at time.Time) error {
	return r.db.Model(&models.InstitutionInvitation{}).
		Where("id = ?", id).
		Update("last_sent_at", at).Error
}

func (r *invitationRepository) CountActiveForInstitution(institutionID uint, now time.Time) (int64, error) {
	var count int64
	err := TenantScope(r.db.Model(&models.InstitutionInvitation{}), institutionID).
		Where("accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?", now).
		Count(&count).Error
	return count, err
}
