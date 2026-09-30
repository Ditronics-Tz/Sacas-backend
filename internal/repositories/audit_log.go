package repositories

import (
	"time"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// AuditLogFilter narrows a trail query. The zero value means "everything".
//
// Every field is optional so one repository method serves the platform view
// (all institutions), a single institution's trail, and one object's history.
type AuditLogFilter struct {
	// InstitutionID scopes to one tenant. PlatformScope (0) means every
	// institution; a nil *uint would be ambiguous between "all" and "none", so
	// the pointer is the flag and the value is the scope.
	InstitutionID *uint
	// ActorID restricts to one user — the answer to "what did this person do".
	ActorID *uint
	// Action and Outcome are exact-match filters.
	Action  *models.AuditAction
	Outcome *models.AuditOutcome
	// TargetType / TargetID restrict to one object's history.
	TargetType string
	TargetID   string
	// From and To bound RecordedAt. Zero means unbounded.
	From time.Time
	To   time.Time
}

// AuditLogRepository writes and reads the audit trail.
//
// There is intentionally no Update and no Delete. A trail that can be edited
// after the fact cannot be used to answer "who changed this, and when", which
// is the only reason to keep one.
type AuditLogRepository interface {
	// Create writes one entry. It never returns an error to the caller for a
	// failed audit write in the request path: losing an audit row must not turn
	// a successful business operation into an error the user sees. Callers in
	// this codebase log the failure instead — see services.AuditRecorder.
	Create(entry *models.AuditLog) error
	// List returns matching entries, newest first.
	List(filter AuditLogFilter, limit, offset int) ([]models.AuditLog, error)
	// Count returns how many entries match, for pagination.
	Count(filter AuditLogFilter) (int64, error)
	// ListForTarget returns the history of one object, oldest first, which is
	// the order a human reads a change history in.
	ListForTarget(institutionID *uint, targetType, targetID string, limit int) ([]models.AuditLog, error)
}

type auditLogRepository struct {
	db *gorm.DB
}

func NewAuditLogRepository(db *gorm.DB) AuditLogRepository {
	return &auditLogRepository{db: db}
}

func (r *auditLogRepository) Create(entry *models.AuditLog) error {
	if entry == nil {
		return gorm.ErrInvalidValue
	}
	if entry.RecordedAt.IsZero() {
		entry.RecordedAt = time.Now().UTC()
	}
	return r.db.Create(entry).Error
}

// apply builds the WHERE clause for a filter. Shared by List and Count so the
// two can never disagree about which rows they mean.
func apply(query *gorm.DB, filter AuditLogFilter) *gorm.DB {
	if filter.InstitutionID != nil {
		if IsPlatformScope(*filter.InstitutionID) {
			// Every institution, including platform actions (NULL). No filter.
		} else {
			query = query.Where("institution_id = ?", *filter.InstitutionID)
		}
	}
	if filter.ActorID != nil {
		query = query.Where("actor_id = ?", *filter.ActorID)
	}
	if filter.Action != nil {
		query = query.Where("action = ?", *filter.Action)
	}
	if filter.Outcome != nil {
		query = query.Where("outcome = ?", *filter.Outcome)
	}
	if filter.TargetType != "" {
		query = query.Where("target_type = ?", filter.TargetType)
	}
	if filter.TargetID != "" {
		query = query.Where("target_id = ?", filter.TargetID)
	}
	if !filter.From.IsZero() {
		query = query.Where("recorded_at >= ?", filter.From)
	}
	if !filter.To.IsZero() {
		query = query.Where("recorded_at <= ?", filter.To)
	}
	return query
}

func (r *auditLogRepository) List(filter AuditLogFilter, limit, offset int) ([]models.AuditLog, error) {
	var entries []models.AuditLog
	query := apply(r.db.Model(&models.AuditLog{}), filter).
		Order("recorded_at DESC, id DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	err := query.Find(&entries).Error
	return entries, err
}

func (r *auditLogRepository) Count(filter AuditLogFilter) (int64, error) {
	var count int64
	err := apply(r.db.Model(&models.AuditLog{}), filter).Count(&count).Error
	return count, err
}

func (r *auditLogRepository) ListForTarget(institutionID *uint, targetType, targetID string, limit int) ([]models.AuditLog, error) {
	var entries []models.AuditLog
	// Use the recorded ID rather than the displayed string: it is exact, and it
	// does not change if a record is renamed.
	query := r.db.Model(&models.AuditLog{}).
		Where("target_type = ? AND target_id = ?", targetType, targetID)
	if institutionID != nil && !IsPlatformScope(*institutionID) {
		query = query.Where("institution_id = ?", *institutionID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Order("recorded_at ASC, id ASC").Find(&entries).Error
	return entries, err
}
