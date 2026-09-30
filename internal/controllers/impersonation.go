package controllers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
)

// ImpersonationController exposes audited, read-only support access into an
// institution.
//
// This is the ONLY path by which a platform operator can see inside an
// institution's workspace. It exists because the permission model deliberately
// gives super_admin no institution-workspace permissions at all: a platform
// account is not bound to an institution, so it has no tenant to scope queries
// with. Support needs that visibility, so it is granted deliberately, narrowly,
// and on the record.
type ImpersonationController struct {
	service *services.ImpersonationService
	audit   *services.AuditRecorder
}

func NewImpersonationController(
	service *services.ImpersonationService,
	audit *services.AuditRecorder,
) *ImpersonationController {
	return &ImpersonationController{service: service, audit: audit}
}

// Start handles POST /superadmin/institutions/:id/impersonate.
//
// The audit entry is written by the service before the token is returned, so a
// live support session is never unrecorded.
func (c *ImpersonationController) Start(ctx *gin.Context) {
	if c.service == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Support access is unavailable"})
		return
	}

	institutionID, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution ID"})
		return
	}

	actor, ok := currentUserFromContext(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	session, err := c.service.Start(actor, institutionID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrImpersonationDisabled):
			ctx.JSON(http.StatusForbidden, gin.H{
				"error":   "Support access is disabled",
				"details": "Set IM_PERSONATION_ENABLED=true to allow audited support sessions.",
			})
		case errors.Is(err, services.ErrImpersonationNotAllowed):
			ctx.JSON(http.StatusForbidden, gin.H{"error": "Support access refused", "details": err.Error()})
		default:
			logger.Error("Failed to start impersonation: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start support session"})
		}
		return
	}

	logger.Warn("Impersonation started: actor=%s institution=%d expires=%s",
		actor.Email, session.InstitutionID, session.ExpiresAt.Format(time.RFC3339))

	ctx.JSON(http.StatusCreated, gin.H{
		"message": "Read-only support session created. Every action taken with this token is recorded in the audit log.",
		"session": session,
	})
}

// End handles POST /superadmin/institutions/:id/impersonate/end.
//
// Tokens are short-lived and stateless, so ending a session cannot invalidate
// the token server-side. What this does is record the end, so a session that
// was never closed is visible in the trail as a start with no matching end.
func (c *ImpersonationController) End(ctx *gin.Context) {
	if c.service == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Support access is unavailable"})
		return
	}

	institutionID, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution ID"})
		return
	}
	actor, ok := currentUserFromContext(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	if err := c.service.End(actor, institutionID); err != nil {
		logger.Error("Failed to end impersonation: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to end support session"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message":        "Support session closed and recorded",
		"institution_id": institutionID,
	})
}

// currentUserFromContext returns the authenticated user as TenantMiddleware
// loaded it. Used where the controller needs the actor's identity for the audit
// trail rather than just an ID.
func currentUserFromContext(ctx *gin.Context) (*models.User, bool) {
	raw, ok := ctx.Get("user")
	if !ok || raw == nil {
		return nil, false
	}
	user, ok := raw.(*models.User)
	if !ok || user == nil {
		return nil, false
	}
	return user, true
}
