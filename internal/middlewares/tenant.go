package middlewares

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
	"go_boilerplate/pkg/logger"
)

// Context keys for the resolved tenant. Handlers must read the tenant from
// here and never from a request body, a header, or the JWT claims — the
// values below are loaded from the database on every request.
const (
	ContextKeyInstitutionID = "institution_id"
	ContextKeyUserID        = "user_id"
	ContextKeyRole          = "role"
	ContextKeyEmail         = "email"
	ContextKeyUser          = "user"
)

// PlatformScope is the InstitutionID used for platform super_admins, who are
// not bound to a single institution and therefore see every tenant. It is
// never a real institution ID — real institutions start at 1.
const PlatformScope uint = 0

// IsPlatformScope reports whether an institution ID means "all institutions".
func IsPlatformScope(institutionID uint) bool { return institutionID == PlatformScope }

// SetTenant stores the resolved tenant on the request context.
func SetTenant(c *gin.Context, institutionID uint, role string) {
	c.Set(ContextKeyInstitutionID, institutionID)
	if role != "" {
		c.Set(ContextKeyRole, role)
	}
}

// InstitutionIDFromContext returns the institution ID resolved by
// TenantMiddleware. It returns PlatformScope for a platform super_admin and 0
// for an unauthenticated request that never passed through the middleware.
func InstitutionIDFromContext(c *gin.Context) uint {
	raw, exists := c.Get(ContextKeyInstitutionID)
	if !exists {
		return PlatformScope
	}
	switch v := raw.(type) {
	case uint:
		return v
	case uint64:
		return uint(v)
	case int:
		return uint(v)
	case int64:
		return uint(v)
	case *uint:
		if v == nil {
			return PlatformScope
		}
		return *v
	default:
		var n uint64
		if _, err := fmt.Sscanf(fmt.Sprint(v), "%d", &n); err != nil {
			return PlatformScope
		}
		return uint(n)
	}
}

// RoleFromContext returns the role loaded from the database. The JWT role
// claim is deliberately overwritten by the tenant middleware so a role change
// or a deactivation takes effect immediately instead of at token expiry.
func RoleFromContext(c *gin.Context) string {
	if r, ok := roleFromContext(c); ok {
		return r
	}
	return ""
}

// IsPlatformUser reports whether the request is made by a platform
// super_admin, i.e. a super_admin that is not bound to an institution.
func IsPlatformUser(c *gin.Context) bool {
	return RoleFromContext(c) == string(models.RoleSuperAdmin) &&
		InstitutionIDFromContext(c) == PlatformScope
}

// UserIDFromContext returns the authenticated user's ID as a uint, coercing
// across the numeric types a JSON/JWT claim may decode to.
func UserIDFromContext(c *gin.Context) (uint, bool) {
	raw, exists := c.Get(ContextKeyUserID)
	if !exists || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case uint:
		return v, true
	case uint64:
		return uint(v), true
	case int:
		return uint(v), true
	case int64:
		return uint(v), true
	case float64:
		return uint(v), true
	default:
		var n uint64
		if _, err := fmt.Sscanf(fmt.Sprint(v), "%d", &n); err != nil {
			return 0, false
		}
		return uint(n), true
	}
}

// TenantLookup resolves the institution a user belongs to. It is supplied by
// the caller (a repository) so the middleware stays free of persistence
// concerns and is trivial to fake in tests.
type TenantLookup func(institutionID uint) (*models.Institution, error)

// TenantMiddleware resolves the acting tenant from the database and pins it,
// the user ID, and the current role onto the request context.
//
// It must run after JWTAuthMiddleware. It does four things:
//
//  1. Loads the user row and overwrites the JWT-derived user_id, role, and
//     email, so a demotion or deactivation applies on the very next request.
//  2. Resolves institution_id from the user row. A super_admin with no
//     institution gets PlatformScope (all tenants); everyone else is pinned to
//     their own institution.
//  3. Rejects users of a non-active institution (pending, suspended, or
//     expired trial).
//  4. Rejects a request that carries a tenant the user does not belong to.
func TenantMiddleware(lookup ActiveUserLookup, institutions TenantLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		if lookup == nil {
			c.Next()
			return
		}
		raw, exists := c.Get(ContextKeyUserID)
		if !exists || raw == nil {
			// Unauthenticated: leave the context untouched and let the
			// downstream handler decide.
			c.Next()
			return
		}
		var id uint
		switch v := raw.(type) {
		case float64:
			id = uint(v)
		case int:
			id = uint(v)
		case uint:
			id = v
		case int64:
			id = uint(v)
		case uint64:
			id = uint(v)
		default:
			var n uint64
			if _, err := fmt.Sscanf(fmt.Sprint(v), "%d", &n); err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user in token"})
				c.Abort()
				return
			}
			id = uint(n)
		}

		user, err := lookup(id)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			c.Abort()
			return
		}
		if !user.IsActive {
			c.JSON(http.StatusForbidden, gin.H{"error": "Account is not active"})
			c.Abort()
			return
		}

		// The DB is the source of truth for identity and role, so overwrite
		// whatever the token claimed.
		c.Set(ContextKeyUserID, user.ID)
		c.Set(ContextKeyRole, string(user.Role))
		c.Set(ContextKeyEmail, user.Email)
		c.Set(ContextKeyUser, user)

		if user.InstitutionID == nil {
			// No institution. Only a platform super_admin may operate across
			// all tenants; an ordinary user with no institution is a
			// misconfiguration, not a cross-tenant request.
			if user.Role != models.RoleSuperAdmin {
				logger.Error("User %d (role %s) has no institution — refusing request", user.ID, user.Role)
				c.JSON(http.StatusForbidden, gin.H{"error": "Account is not assigned to an institution"})
				c.Abort()
				return
			}
			SetTenant(c, PlatformScope, string(user.Role))
			c.Next()
			return
		}

		institutionID := *user.InstitutionID
		if institutions != nil {
			inst, err := institutions(institutionID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve institution"})
				c.Abort()
				return
			}
			if inst == nil {
				logger.Error("User %d references missing institution %d", user.ID, institutionID)
				c.JSON(http.StatusForbidden, gin.H{"error": "Institution not found"})
				c.Abort()
				return
			}
			if !inst.IsActive(nowFunc()) {
				c.JSON(http.StatusForbidden, gin.H{
					"error":   "Institution is not active",
					"status":  inst.Status,
					"message": institutionMessage(inst),
				})
				c.Abort()
				return
			}
		}
		SetTenant(c, institutionID, string(user.Role))
		c.Next()
	}
}

// nowFunc is overridable in tests.
var nowFunc = func() time.Time { return time.Now() }

// institutionMessage gives an operator-facing hint about why access was denied.
func institutionMessage(inst *models.Institution) string {
	if inst.Status == models.InstitutionStatusSuspended {
		return "This institution's account is suspended. Contact the platform administrator."
	}
	if inst.Status == models.InstitutionStatusPending {
		return "This institution is awaiting activation."
	}
	if inst.TrialExpired(nowFunc()) {
		return "This institution's trial has expired. Contact the platform administrator."
	}
	return "This institution is not active."
}

// RequirePlatformOnly rejects any request that is not a platform super_admin.
// Use it on the institution management endpoints.
func RequirePlatformOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsPlatformUser(c) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Platform administrator access required"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// Slugify converts a human name into a URL-safe slug used by Institution.Slug.
func Slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
