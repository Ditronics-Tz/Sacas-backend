package controllers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
	"go_boilerplate/pkg/security"
)

// SuperAdminUserController serves the platform's user administration.
//
// This is the only place a role other than the institution-scoped set is
// managed, which is why it is gated on a platform workspace rather than an
// institution one: a tenant admin has no business enumerating accounts across
// tenants, and a tenant's own list lives at /protected/institution/members.
type SuperAdminUserController struct {
	userRepo        repositories.UserRepository
	institutionRepo repositories.InstitutionRepository
	notify          *services.NotificationService
	audit           *services.AuditRecorder
}

func NewSuperAdminUserController(
	userRepo repositories.UserRepository,
	institutionRepo repositories.InstitutionRepository,
	notify *services.NotificationService,
	audit *services.AuditRecorder,
) *SuperAdminUserController {
	return &SuperAdminUserController{
		userRepo:        userRepo,
		institutionRepo: institutionRepo,
		notify:          notify,
		audit:           audit,
	}
}

// ListUsers handles GET /api/protected/superadmin/users.
//
// Unlike an institution listing, this INCLUDES platform accounts
// (institution_id IS NULL): a platform administrator needs to see the
// super_admins, and the tenant repository deliberately excludes them.
func (c *SuperAdminUserController) ListUsers(ctx *gin.Context) {
	limit, offset := parsePagination(ctx)
	roleFilter := ctx.Query("role")
	instFilter := ctx.Query("institution_id")
	activeFilter := ctx.Query("is_active")

	var users []models.User
	var err error

	// PlatformScope (0) means "no tenant filter", which is exactly what is
	// wanted here: a platform read spans every institution.
	users, err = c.listUsers(repositories.PlatformScope, roleFilter, limit, offset)
	if err != nil {
		logger.Error("Failed to list platform users: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list users"})
		return
	}

	// Optional narrowing, applied in memory because the tenant repository's
	// listing already excludes platform accounts and this view needs both.
	if instFilter != "" {
		parsed, perr := strconv.ParseUint(instFilter, 10, 32)
		if perr != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid institution_id"})
			return
		}
		id := uint(parsed)
		filtered := users[:0:0]
		for _, u := range users {
			if id == 0 && u.InstitutionID == nil {
				filtered = append(filtered, u)
				continue
			}
			if u.InstitutionID != nil && *u.InstitutionID == id {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	}
	if activeFilter != "" {
		active, perr := strconv.ParseBool(activeFilter)
		if perr != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid is_active"})
			return
		}
		filtered := users[:0:0]
		for _, u := range users {
			if u.IsActive == active {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	}

	ctx.JSON(http.StatusOK, gin.H{
		"users":  users,
		"limit":  limit,
		"offset": offset,
		"note":   "Includes platform accounts (institution_id null) as well as every institution's members.",
	})
}

// listUsers fetches a page, optionally by role.
func (c *SuperAdminUserController) listUsers(scope uint, role string, limit, offset int) ([]models.User, error) {
	if role != "" {
		return c.userRepo.GetByRole(scope, role, limit, offset)
	}
	return c.userRepo.GetAll(scope, limit, offset)
}

// GetUser handles GET /api/protected/superadmin/users/:id.
func (c *SuperAdminUserController) GetUser(ctx *gin.Context) {
	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	user, err := c.userRepo.GetByID(id)
	if err != nil {
		respondRepoError(ctx, "User not found", err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"user": user})
}

type PlatformUpdateUserRequest struct {
	Email     *string `json:"email,omitempty" binding:"omitempty,email"`
	FirstName *string `json:"first_name,omitempty" binding:"omitempty,min=2,max=50"`
	LastName  *string `json:"last_name,omitempty" binding:"omitempty,min=2,max=50"`
	Phone     *string `json:"phone,omitempty"`
	Role      *string `json:"role,omitempty"`
	// IsActive is the platform's suspend/reactivate switch.
	IsActive *bool `json:"is_active,omitempty"`
	// InstitutionID moves an account between institutions. NULL makes it a
	// platform account. Guarded below: a super_admin must not be bound to an
	// institution.
	InstitutionID *uint `json:"institution_id,omitempty"`
	// MakePlatform is the explicit way to clear institution_id, since a JSON
	// null and an absent key are otherwise indistinguishable.
	MakePlatform bool `json:"make_platform,omitempty"`
}

// UpdateUser handles PUT /api/protected/superadmin/users/:id.
func (c *SuperAdminUserController) UpdateUser(ctx *gin.Context) {
	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	user, err := c.userRepo.GetByID(id)
	if err != nil {
		respondRepoError(ctx, "User not found", err)
		return
	}

	var req PlatformUpdateUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	previousRole := user.Role
	previousActive := user.IsActive
	previousInstitution := user.InstitutionID

	// Work out the target institution first, because the role rules depend on it.
	targetInstitution := previousInstitution
	if req.MakePlatform {
		targetInstitution = nil
	} else if req.InstitutionID != nil {
		if *req.InstitutionID == 0 {
			targetInstitution = nil
		} else {
			// The named institution must exist, or the account would be bound
			// to nothing usable.
			if _, err := c.institutionRepo.GetByID(*req.InstitutionID); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "Institution not found"})
				return
			}
			id := *req.InstitutionID
			targetInstitution = &id
		}
	}

	if req.Role != nil {
		newRole := models.UserRole(*req.Role)
		if !newRole.IsValid() {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role"})
			return
		}
		// The support role belongs to a support token, never to a row.
		if newRole == models.RoleSupport {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "This role cannot be assigned to an account",
				"details": "institution_support exists only inside a short-lived support token.",
			})
			return
		}
		// A super_admin with no institution is a platform account; one bound to
		// an institution is a tenant user the workspace gate would refuse.
		if newRole == models.RoleSuperAdmin && targetInstitution != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "A super_admin must not be bound to an institution",
				"details": "Set make_platform to true, or choose a different role.",
			})
			return
		}
		user.Role = newRole
	}

	if req.Email != nil {
		newEmail := strings.ToLower(strings.TrimSpace(*req.Email))
		if existing, err := c.userRepo.GetByEmail(newEmail); err == nil && existing != nil && existing.ID != user.ID {
			ctx.JSON(http.StatusConflict, gin.H{"error": "An account with this email already exists"})
			return
		}
		user.Email = newEmail
	}
	if req.FirstName != nil {
		user.FirstName = *req.FirstName
	}
	if req.LastName != nil {
		user.LastName = *req.LastName
	}
	if req.Phone != nil {
		user.PhoneNumber = *req.Phone
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}
	// Scope the ordinary update by the user's CURRENT institution, not the
	// target. The scope is the guard: it proves the account is where the
	// platform already believes it is, and using the target here would make the
	// update match zero rows for any actual move.
	scope := repositories.PlatformScope
	if previousInstitution != nil {
		scope = *previousInstitution
	}
	if err := c.userRepo.Update(scope, user); err != nil {
		logger.Error("Failed to update platform user %d: %v", id, err)
		respondRepoError(ctx, "User not found", err)
		return
	}

	// institution_id is written through its own method, not the general update:
	// it is a privileged field, and letting a profile update move an account
	// between tenants would break the isolation the whole tenancy model rests on.
	if institutionChanged(previousInstitution, targetInstitution) {
		if err := c.userRepo.SetInstitution(id, targetInstitution); err != nil {
			logger.Error("Failed to move user %d between institutions: %v", id, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to move the account between institutions"})
			return
		}
		user.InstitutionID = targetInstitution
	}

	detail := map[string]any{"target_role": string(user.Role), "is_active": user.IsActive}
	if previousRole != user.Role {
		detail["role_from"] = string(previousRole)
		detail["role_to"] = string(user.Role)
		c.audit.Record(ctx, models.AuditRoleChange, "user", uintString(user.ID), user.Email, detail)
	}
	if previousActive != user.IsActive {
		action := models.AuditUserActivate
		if !user.IsActive {
			action = models.AuditUserSuspend
		}
		c.audit.Record(ctx, action, "user", uintString(user.ID), user.Email, detail)
	}
	if institutionChanged(previousInstitution, targetInstitution) {
		c.audit.Record(ctx, models.AuditUserUpdate, "user", uintString(user.ID), user.Email, map[string]any{
			"operation":        "move_institution",
			"from_institution": institutionDetail(previousInstitution),
			"to_institution":   institutionDetail(targetInstitution),
		})
	}
	c.audit.Record(ctx, models.AuditUserUpdate, "user", uintString(user.ID), user.Email, detail)

	ctx.JSON(http.StatusOK, gin.H{"message": "User updated", "user": user})
}

// SetUserStatus handles POST /api/protected/superadmin/users/:id/status —
// suspend or reactivate.
//
// A separate endpoint from the general update because suspension is the action a
// support engineer performs under pressure, and it deserves one obvious URL that
// is easy to find, easy to audit, and hard to trigger by accident.
func (c *SuperAdminUserController) SetUserStatus(ctx *gin.Context) {
	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var req struct {
		// A pointer, not a bool: Gin's `required` treats false as the zero
		// value, so a plain `bool` with `binding:"required"` would make
		// suspension — the whole point of this endpoint — impossible to express.
		IsActive *bool  `json:"is_active" binding:"required"`
		Reason   string `json:"reason,omitempty"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}
	wantActive := *req.IsActive

	user, err := c.userRepo.GetByID(id)
	if err != nil {
		respondRepoError(ctx, "User not found", err)
		return
	}

	// The platform must not be able to lock itself out by deactivating the last
	// platform account, or a mistake would need database access to undo.
	if !wantActive && user.Role == models.RoleSuperAdmin && user.InstitutionID == nil {
		if remaining, err := c.countActivePlatformSuperAdmins(id); err == nil && remaining == 0 {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "This is the only active platform administrator",
				"details": "Activate or create another super_admin before suspending this account.",
			})
			return
		}
	}

	// Scope by the user's own institution so the write cannot be redirected.
	scope := repositories.PlatformScope
	if user.InstitutionID != nil {
		scope = *user.InstitutionID
	}
	if wantActive {
		err = c.userRepo.ActivateUser(scope, id)
	} else {
		err = c.userRepo.DeactivateUser(scope, id)
	}
	if err != nil {
		logger.Error("Failed to set status for user %d: %v", id, err)
		respondRepoError(ctx, "User not found", err)
		return
	}

	action := models.AuditUserActivate
	if !wantActive {
		action = models.AuditUserSuspend
	}
	c.audit.Record(ctx, action, "user", uintString(id), user.Email, map[string]any{
		"is_active": wantActive,
		"reason":    req.Reason,
		"source":    "platform",
	})

	ctx.JSON(http.StatusOK, gin.H{
		"message":   statusMessage(wantActive),
		"user_id":   id,
		"is_active": wantActive,
	})
}

type PlatformResetPasswordRequest struct {
	// NewPassword sets a password directly. When empty, a random one is
	// generated and emailed, which is the safer default: the platform operator
	// never handles the new secret.
	NewPassword string `json:"new_password,omitempty"`
}

// ResetPassword handles POST /api/protected/superadmin/users/:id/reset-password.
//
// A password reset is a takeover primitive, so it is deliberately awkward:
// by default the new password is generated server-side and emailed to the
// account holder, never returned in the response. Supplying one explicitly is
// allowed for a break-glass case and is recorded in the audit trail with a
// severity marker.
func (c *SuperAdminUserController) ResetPassword(ctx *gin.Context) {
	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var req PlatformResetPasswordRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	user, err := c.userRepo.GetByID(id)
	if err != nil {
		respondRepoError(ctx, "User not found", err)
		return
	}

	generated := false
	password := req.NewPassword
	if password == "" {
		password, err = services.GeneratePassword(16)
		if err != nil {
			logger.Error("Failed to generate password: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
			return
		}
		generated = true
	}
	if !security.ValidPassword(password) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("Failed to hash password: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}
	if err := c.userRepo.UpdatePassword(id, string(hashed)); err != nil {
		logger.Error("Failed to store password for user %d: %v", id, err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}

	detail := map[string]any{
		"source":           "platform",
		"generated":        generated,
		"severity":         "high",
		"password_in_body": !generated,
	}
	c.audit.Record(ctx, models.AuditPasswordReset, "user", uintString(id), user.Email, detail)

	// The generated password is emailed, never returned. This is the whole point
	// of the default path.
	response := gin.H{
		"message":   "Password reset.",
		"user_id":   id,
		"generated": generated,
	}
	if generated {
		if c.notify != nil {
			if err := c.notify.SendPasswordReset(user.Email, password); err != nil {
				logger.Error("Failed to email password reset to %s: %v", user.Email, err)
				response["message"] = "Password reset, but the notification could not be sent. Ask the user to use 'forgot password' instead."
			}
		}
	} else {
		response["note"] = "An explicit password was set and has not been emailed."
	}

	logger.Warn("Platform password reset for user %d (%s), generated=%v", id, user.Email, generated)
	ctx.JSON(http.StatusOK, response)
}

// countActivePlatformSuperAdmins counts active platform super_admins other than
// the given user.
func (c *SuperAdminUserController) countActivePlatformSuperAdmins(exclude uint) (int, error) {
	users, err := c.userRepo.GetByRole(repositories.PlatformScope, string(models.RoleSuperAdmin), maxPageSize, 0)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, u := range users {
		if u.ID == exclude {
			continue
		}
		// Only genuinely platform accounts count; a super_admin bound to an
		// institution is a tenant user and cannot administer the platform.
		if u.InstitutionID == nil && u.IsActive {
			count++
		}
	}
	return count, nil
}

func institutionChanged(before, after *uint) bool {
	switch {
	case before == nil && after == nil:
		return false
	case before == nil || after == nil:
		return true
	default:
		return *before != *after
	}
}

func statusMessage(active bool) string {
	if active {
		return "Account reactivated"
	}
	return "Account suspended"
}
