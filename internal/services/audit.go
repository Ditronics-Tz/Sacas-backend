package services

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

// AuditRecorder writes audit entries for the request path.
//
// The governing trade-off: a consequential action must not be reported to the
// user as failed (or, worse, rolled back) because the audit write failed, and a
// lost audit row must not be silent. So Record logs a write failure loudly and
// returns void — the business operation has already happened, and failing the
// HTTP response afterwards would leave the caller believing it did not.
type AuditRecorder struct {
	repo repositories.AuditLogRepository
	// clock is injectable so tests can assert ordering.
	clock func() time.Time
}

// NewAuditRecorder builds a recorder. A nil repo is tolerated so routes can be
// wired in tests without a database; entries are then dropped with a warning
// rather than causing a nil dereference.
func NewAuditRecorder(repo repositories.AuditLogRepository) *AuditRecorder {
	return &AuditRecorder{repo: repo, clock: func() time.Time { return time.Now().UTC() }}
}

// Record writes one audit entry, taking the actor and institution from the
// request context. The actor is the authenticated user resolved by
// TenantMiddleware, never anything read from the request body.
func (a *AuditRecorder) Record(
	c *gin.Context,
	action models.AuditAction,
	targetType, targetID, target string,
	detail map[string]any,
) {
	if a == nil || a.repo == nil {
		return
	}
	entry := a.build(c, action, targetType, targetID, target, detail)
	a.write(entry)
}

// RecordDenied writes a refused action. Call this from the same place as
// Record so a denial is as visible as a success.
func (a *AuditRecorder) RecordDenied(
	c *gin.Context,
	action models.AuditAction,
	targetType, targetID, target string,
	reason string,
) {
	if a == nil || a.repo == nil {
		return
	}
	entry := a.build(c, action, targetType, targetID, target, nil)
	entry.Outcome = models.AuditOutcomeDenied
	if reason != "" {
		entry.Detail = reason
	}
	a.write(entry)
}

// RecordSystem writes an entry with no request context, for actions performed
// by the system itself (migrations, scheduled jobs).
func (a *AuditRecorder) RecordSystem(
	action models.AuditAction,
	institutionID *uint,
	targetType, targetID, target string,
	detail map[string]any,
) {
	if a == nil || a.repo == nil {
		return
	}
	entry := &models.AuditLog{
		RecordedAt:    a.clock(),
		InstitutionID: institutionID,
		Action:        action,
		Outcome:       models.AuditOutcomeSuccess,
		TargetType:    targetType,
		TargetID:      targetID,
		Target:        target,
		Detail:        encodeDetail(detail),
	}
	a.write(entry)
}

// build assembles the entry from the request context.
func (a *AuditRecorder) build(
	c *gin.Context,
	action models.AuditAction,
	targetType, targetID, target string,
	detail map[string]any,
) *models.AuditLog {
	entry := &models.AuditLog{
		RecordedAt: a.clock(),
		Action:     action,
		Outcome:    models.AuditOutcomeSuccess,
		TargetType: targetType,
		TargetID:   targetID,
		Target:     target,
		Detail:     encodeDetail(detail),
	}
	if c == nil {
		return entry
	}

	if user, ok := c.Get("user"); ok {
		if u, ok := user.(*models.User); ok && u != nil {
			entry.ActorID = u.ID
			entry.ActorEmail = u.Email
			entry.ActorRole = string(u.Role)
		}
	}
	// Fall back to the resolved context keys when the user object is absent
	// (for example on a route that did not populate it).
	if entry.ActorID == 0 {
		if id, ok := c.Get("user_id"); ok {
			entry.ActorID = toUint(id)
		}
	}
	if entry.ActorRole == "" {
		if role, ok := c.Get("role"); ok {
			entry.ActorRole = toString(role)
		}
	}
	if entry.ActorEmail == "" {
		if email, ok := c.Get("email"); ok {
			entry.ActorEmail = toString(email)
		}
	}

	// A platform action has no institution; a tenant action has one.
	instID := institutionFromContext(c)
	entry.InstitutionID = instID

	entry.IPAddress = c.ClientIP()
	entry.UserAgent = c.Request.UserAgent()
	return entry
}

func (a *AuditRecorder) write(entry *models.AuditLog) {
	if err := a.repo.Create(entry); err != nil {
		// Deliberately not propagated: see the type comment. Losing the audit
		// row is bad, but failing an already-committed business operation is
		// worse, and a loud log makes the gap visible to an operator.
		logger.Error("AUDIT WRITE FAILED action=%s target=%s: %v", entry.Action, entry.Target, err)
	}
}

// encodeDetail renders the detail map as JSON. A value that cannot be encoded
// is replaced with a short note rather than dropping the whole entry, because a
// row with a missing field is more useful than no row.
func encodeDetail(detail map[string]any) string {
	if len(detail) == 0 {
		return ""
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return `{"_encode_error":"detail could not be serialised"}`
	}
	return string(raw)
}

// institutionFromContext returns a pointer to the request's institution, or nil
// for a platform request. Returning nil for platform is what distinguishes a
// platform action in the trail.
func institutionFromContext(c *gin.Context) *uint {
	if c == nil {
		return nil
	}
	raw, ok := c.Get("institution_id")
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case uint:
		if v == 0 {
			return nil
		}
		return &v
	case *uint:
		if v == nil || *v == 0 {
			return nil
		}
		return v
	default:
		n := toUint(raw)
		if n == 0 {
			return nil
		}
		return &n
	}
}
