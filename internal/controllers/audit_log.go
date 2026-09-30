package controllers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
)

// AuditLogController exposes the audit trail.
//
// Two access shapes:
//
//	/platform  — a platform super_admin reads every institution's trail, and
//	             can narrow to one with ?institution_id=.
//	/tenant    — an institution reads its own trail only. The scope comes from
//	             the session, never from the query, so a tenant cannot read
//	             another tenant's history by passing a different id.
type AuditLogController struct {
	repo  repositories.AuditLogRepository
	audit *services.AuditRecorder
}

func NewAuditLogController(repo repositories.AuditLogRepository, audit *services.AuditRecorder) *AuditLogController {
	return &AuditLogController{repo: repo, audit: audit}
}

// List handles GET .../audit.
//
// Query parameters: institution_id, actor_id, action, outcome, target_type,
// target_id, from, to, limit, offset.
func (c *AuditLogController) List(ctx *gin.Context) {
	filter, ok := c.parseFilter(ctx)
	if !ok {
		return
	}

	limit, offset := parsePagination(ctx)
	entries, err := c.repo.List(filter, limit, offset)
	if err != nil {
		logger.Error("Failed to list audit log: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load audit log"})
		return
	}
	total, err := c.repo.Count(filter)
	if err != nil {
		logger.Warn("Failed to count audit log: %v", err)
	}

	ctx.JSON(http.StatusOK, gin.H{
		"entries": entries,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// ListForTarget handles GET .../audit/target/:type/:id — the change history of
// one object, oldest first, which is the order a human reads it in.
func (c *AuditLogController) ListForTarget(ctx *gin.Context) {
	targetType := ctx.Param("type")
	targetID := ctx.Param("id")
	if targetType == "" || targetID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Target type and id are required"})
		return
	}

	institutionID, _, ok := c.scopeFor(ctx)
	if !ok {
		return
	}

	entries, err := c.repo.ListForTarget(institutionID, targetType, targetID, maxAuditPage)
	if err != nil {
		logger.Error("Failed to list target audit log: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load audit log"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"entries": entries, "target_type": targetType, "target_id": targetID})
}

// maxAuditPage bounds a single-object history so one long-lived record cannot
// return an unbounded list.
const maxAuditPage = 200

// parseFilter turns query parameters into a repository filter, enforcing the
// caller's scope. Returns false when the request should not proceed.
func (c *AuditLogController) parseFilter(ctx *gin.Context) (repositories.AuditLogFilter, bool) {
	var filter repositories.AuditLogFilter
	institutionID, isPlatform, ok := c.scopeFor(ctx)
	if !ok {
		return filter, false
	}
	filter.InstitutionID = institutionID

	if raw := ctx.Query("institution_id"); raw != "" {
		// Only a platform caller may narrow across institutions. A tenant asking
		// for another institution is told so rather than silently overridden, so
		// the behaviour is not surprising.
		if !isPlatform {
			ctx.JSON(http.StatusForbidden, gin.H{"error": "Only a platform administrator may filter by institution"})
			return filter, false
		}
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution_id"})
			return filter, false
		}
		id := uint(parsed)
		filter.InstitutionID = &id
	}

	if raw := ctx.Query("actor_id"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid actor_id"})
			return filter, false
		}
		id := uint(parsed)
		filter.ActorID = &id
	}

	if raw := ctx.Query("action"); raw != "" {
		action := models.AuditAction(raw)
		if !knownAuditAction(action) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Unknown action", "details": "see docs/RBAC_AUDIT.md for the action list"})
			return filter, false
		}
		filter.Action = &action
	}

	if raw := ctx.Query("outcome"); raw != "" {
		outcome := models.AuditOutcome(raw)
		if outcome != models.AuditOutcomeSuccess && outcome != models.AuditOutcomeDenied && outcome != models.AuditOutcomeFailure {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid outcome", "details": "must be success, denied, or failure"})
			return filter, false
		}
		filter.Outcome = &outcome
	}

	filter.TargetType = ctx.Query("target_type")
	filter.TargetID = ctx.Query("target_id")

	// from / to are RFC3339. An unparseable bound is a client error rather
	// than being ignored, because silently ignoring a date filter would
	// return more data than the caller asked for.
	if raw := ctx.Query("from"); raw != "" {
		t, err := parseRFC3339(raw)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid from", "details": "use RFC3339, e.g. 2026-01-31T00:00:00Z"})
			return filter, false
		}
		filter.From = t
	}
	if raw := ctx.Query("to"); raw != "" {
		t, err := parseRFC3339(raw)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid to", "details": "use RFC3339, e.g. 2026-12-31T23:59:59Z"})
			return filter, false
		}
		filter.To = t
	}

	return filter, true
}

// scopeFor returns the institution scope for the caller, plus whether the caller
// is a platform account.
//
// The scope is nil for a platform account, which the repository reads as "every
// institution, including platform actions with no institution". isPlatform is
// returned separately rather than being inferred from that nil, because nil
// already means two different things in this codebase and conflating them is how
// a platform-only filter ends up refusing its own caller.
func (c *AuditLogController) scopeFor(ctx *gin.Context) (institutionID *uint, isPlatform bool, ok bool) {
	role := ctx.GetString("role")
	inst := tenantID(ctx)

	if role == string(models.RoleSuperAdmin) && repositories.IsPlatformScope(inst) {
		return nil, true, true
	}
	// A tenant, or a support session: pinned to its own institution.
	scoped := inst
	return &scoped, false, true
}

// knownAuditAction keeps the action filter honest. An unknown action is
// rejected with a helpful message rather than returning an empty page that
// looks like "no such activity happened".
func knownAuditAction(a models.AuditAction) bool {
	for _, known := range allAuditActions {
		if known == a {
			return true
		}
	}
	return false
}

// allAuditActions mirrors the model's action constants. Kept beside the
// controller so the filter validates against the same set the model defines.
var allAuditActions = []models.AuditAction{
	models.AuditRoleChange, models.AuditUserCreate, models.AuditUserUpdate,
	models.AuditUserDelete, models.AuditUserSuspend, models.AuditUserActivate,
	models.AuditPasswordReset, models.AuditRoleSelfElevate,
	models.AuditInstitutionCreate, models.AuditInstitutionUpdate,
	models.AuditInstitutionSuspend, models.AuditInstitutionDelete,
	models.AuditInstitutionPlanChange,
	models.AuditImpersonateStart, models.AuditImpersonateEnd,
	models.AuditTimetableGenerate, models.AuditTimetablePublish,
	models.AuditTimetableApprove, models.AuditExamPublish, models.AuditExamApprove,
	models.AuditDataImport,
}
