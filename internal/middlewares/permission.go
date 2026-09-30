package middlewares

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/auth"
	"go_boilerplate/internal/models"
)

// RequirePermission allows the request only when the caller's role holds the
// named permission, per the map in internal/auth.
//
// Prefer this over RequireRole on new routes: it states the capability the
// endpoint needs rather than the set of roles that happen to have it, so
// changing a role's reach does not require touching every route.
//
// The role is read from the request context, which TenantMiddleware has
// overwritten with the value from the user's database row. A header, a query
// parameter, or a body field cannot influence it.
func RequirePermission(permission auth.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := callerRole(c)
		if !ok {
			// No role resolved means the middleware chain is misconfigured.
			// Fail closed.
			c.JSON(http.StatusForbidden, gin.H{"error": "Permission could not be verified"})
			c.Abort()
			return
		}
		if auth.HasPermission(role, permission) {
			c.Next()
			return
		}
		deny(c, role, []auth.Permission{permission})
	}
}

// RequireAnyPermission allows the request when the caller holds at least one of
// the listed permissions. Use where an endpoint genuinely serves several
// alternative capabilities (for example a read that either an owner or an
// administrator may perform).
func RequireAnyPermission(permissions ...auth.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := callerRole(c)
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "Permission could not be verified"})
			c.Abort()
			return
		}
		if auth.HasAnyPermission(role, permissions...) {
			c.Next()
			return
		}
		deny(c, role, permissions)
	}
}

// RequireInstitutionWorkspace rejects a caller that is not bound to an
// institution.
//
// A platform super_admin has no institution, so it has no tenant context to
// scope a query with. Rather than let it through unscoped — which would be the
// cross-tenant leak the tenancy work closed — it is refused. Support access to
// an institution is granted by explicit, audited impersonation, which does
// carry a tenant scope.
//
// This is the mechanism behind "remove super_admin from institution-workspace
// routes": the permission map already withholds workspace permissions from
// super_admin, and this middleware makes the failure explicit and diagnostic.
func RequireInstitutionWorkspace() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := callerRole(c)
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "Permission could not be verified"})
			c.Abort()
			return
		}
		if !role.IsInstitutionRole() {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "This endpoint operates within an institution",
				"reason":  "platform accounts are not bound to an institution",
				"message": "Use audited impersonation to access an institution's workspace.",
			})
			c.Abort()
			return
		}
		if IsPlatformScope(InstitutionIDFromContext(c)) {
			// Defence in depth: a non-platform role must never reach here with
			// no tenant scope, which would make the repository calls unscoped.
			c.JSON(http.StatusForbidden, gin.H{"error": "No institution scope for this request"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequirePlatformWorkspace is the mirror of RequireInstitutionWorkspace: it
// requires a platform account, i.e. a super_admin with no institution.
//
// It is stronger than checking the role alone. A super_admin who has been
// assigned to an institution is a tenant user, and must not manage the tenant
// list, role assignments, or other tenants' settings.
func RequirePlatformWorkspace() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := callerRole(c)
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "Permission could not be verified"})
			c.Abort()
			return
		}
		if !role.IsPlatformRole() {
			c.JSON(http.StatusForbidden, gin.H{"error": "Platform administrator access required"})
			c.Abort()
			return
		}
		if !IsPlatformScope(InstitutionIDFromContext(c)) {
			// A super_admin bound to an institution is not a platform account.
			c.JSON(http.StatusForbidden, gin.H{"error": "Platform administrator access required"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// callerRole returns the caller's role as it was resolved from the database.
func callerRole(c *gin.Context) (models.UserRole, bool) {
	role, ok := roleFromContext(c)
	if !ok {
		return "", false
	}
	return models.UserRole(role), true
}

// deny writes the 403 body. The caller's role is echoed so the frontend can
// explain the refusal, but the caller's permissions are not — that would help
// someone probing the boundary map out the policy.
func deny(c *gin.Context, role models.UserRole, required []auth.Permission) {
	c.JSON(http.StatusForbidden, gin.H{
		"error":     "Insufficient permissions",
		"your_role": string(role),
		"required":  permissionStrings(required),
	})
}

// permissionStrings converts for JSON output.
func permissionStrings(perms []auth.Permission) []string {
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, string(p))
	}
	return out
}

// AdminMiddleware is retained for routes that have not yet been moved to
// RequirePermission. It is equivalent to requiring the institution-admin
// permission, and is superseded for new code.
func AdminMiddleware() gin.HandlerFunc {
	return RequirePermission(auth.PermUserWrite)
}
