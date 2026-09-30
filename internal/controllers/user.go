package controllers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
	"go_boilerplate/pkg/security"

	"go_boilerplate/internal/auth"
)

type UserController struct {
	userRepo repositories.UserRepository
	audit    *services.AuditRecorder
}

// NewUserController builds the controller. The audit recorder is required for
// every path that changes a role, an account's active state, or the user list.
func NewUserController(userRepo repositories.UserRepository, audit *services.AuditRecorder) *UserController {
	return &UserController{
		userRepo: userRepo,
		audit:    audit,
	}
}

// CreateUserRequest represents the request payload for creating a user
type CreateUserRequest struct {
	Email       string          `json:"email" binding:"required,email"`
	Password    string          `json:"password" binding:"required,min=8,strongpassword"`
	FirstName   string          `json:"first_name" binding:"required,min=2,max=50"`
	LastName    string          `json:"last_name" binding:"required,min=2,max=50"`
	PhoneNumber string          `json:"phone_number,omitempty"`
	Role        models.UserRole `json:"role" binding:"required"`
	// InstitutionID is only honoured from a platform super_admin. Any other
	// caller is pinned to their own institution, so a tenant admin cannot
	// create a user inside somebody else's campus.
	InstitutionID *uint `json:"institution_id,omitempty"`
}

// UpdateUserRequest represents the request payload for updating a user
type UpdateUserRequest struct {
	Email       string          `json:"email,omitempty" binding:"omitempty,email"`
	FirstName   string          `json:"first_name,omitempty" binding:"omitempty,min=2,max=50"`
	LastName    string          `json:"last_name,omitempty" binding:"omitempty,min=2,max=50"`
	PhoneNumber string          `json:"phone_number,omitempty"`
	Role        models.UserRole `json:"role,omitempty"`
	IsActive    *bool           `json:"is_active,omitempty"`
}

// ChangePasswordRequest represents the request payload for changing password
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=8,strongpassword"`
}

// GetUsers retrieves a paginated list of users, scoped to the caller's tenant.
func (uc *UserController) GetUsers(c *gin.Context) {
	inst := tenantID(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	role := c.Query("role")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit

	var users []models.User
	var err error

	if role != "" {
		users, err = uc.userRepo.GetByRole(inst, role, limit, offset)
	} else {
		users, err = uc.userRepo.GetAll(inst, limit, offset)
	}

	if err != nil {
		logger.Error("Failed to retrieve users: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve users"})
		return
	}

	logger.Info("Retrieved %d users for page %d", len(users), page)
	c.JSON(http.StatusOK, gin.H{
		"users": users,
		"page":  page,
		"limit": limit,
	})
}

// GetUser retrieves a single user by ID.
//
// A user belonging to another institution is reported as 404, so one campus's
// admin cannot probe for (or confirm) another campus's accounts.
func (uc *UserController) GetUser(c *gin.Context) {
	inst := tenantID(c)

	id, err := parseIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	user, err := uc.userRepo.GetByID(id)
	if err != nil {
		respondRepoError(c, "User not found", err)
		return
	}
	if user.InstitutionID == nil || !repositories.InTenant(*user.InstitutionID, inst) {
		respondNotFound(c, "User not found")
		return
	}

	logger.Info("Retrieved user %d", id)
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// CreateUser creates a new user (admin only)
func (uc *UserController) CreateUser(c *gin.Context) {
	inst := tenantID(c)

	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if strings.Contains(err.Error(), "Password") || strings.Contains(err.Error(), "password") {
			c.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !security.ValidPassword(req.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
		return
	}

	// Validate role
	if !req.Role.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role"})
		return
	}

	// Role assignment policy (see auth.AssignableRoles):
	//
	//   - A platform super_admin may create any role, in any institution. This
	//     is how institution admins get created.
	//   - An institution admin may create only the plain `user` role. Letting a
	//     tenant admin mint another admin or a coordinator would be an
	//     escalation path inside a tenant, so promotion goes through the
	//     platform.
	//   - The support role can never be assigned permanently; it exists only
	//     inside a short-lived support token.
	currentUserRole := models.UserRole(c.GetString("role"))
	if !auth.CanAssign(currentUserRole, req.Role) {
		uc.audit.RecordDenied(c, models.AuditUserCreate, "user", "", req.Email,
			"caller may not assign role "+string(req.Role))
		c.JSON(http.StatusForbidden, gin.H{
			"error":          "You may not assign this role",
			"your_role":      string(currentUserRole),
			"assignable":     roleStrings(auth.AssignableRoles(currentUserRole)),
			"requested_role": string(req.Role),
		})
		return
	}
	if req.Role == models.RoleSupport {
		// Belt and braces: CanAssign already excludes it, but the support role
		// must never be persisted on a user row under any path.
		uc.audit.RecordDenied(c, models.AuditUserCreate, "user", "", req.Email,
			"support role cannot be assigned to a persistent account")
		c.JSON(http.StatusForbidden, gin.H{"error": "This role cannot be assigned to an account"})
		return
	}

	// The target institution is the caller's own, unless a platform
	// super_admin explicitly names another one.
	targetInstitution := inst
	isPlatform := currentUserRole == models.RoleSuperAdmin && repositories.IsPlatformScope(inst)
	if isPlatform {
		if req.InstitutionID != nil {
			targetInstitution = *req.InstitutionID
		} else {
			// A platform super_admin with no explicit target creates a
			// platform-level account (institution_id NULL).
			targetInstitution = repositories.PlatformScope
		}
	}

	// A super_admin role only makes sense for a platform-level account. A
	// super_admin bound to an institution would be a tenant user with platform
	// reach, which the workspace gate would refuse anyway — so refuse it at
	// write time with a clear message instead.
	if req.Role == models.RoleSuperAdmin && !repositories.IsPlatformScope(targetInstitution) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "A super_admin must not be bound to an institution",
			"details": "Create the account without an institution_id to make it a platform account.",
		})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("Failed to hash password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process password"})
		return
	}

	user := &models.User{
		Email:       req.Email,
		Password:    string(hashedPassword),
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		PhoneNumber: req.PhoneNumber,
		Role:        req.Role,
		IsActive:    false, // New users start inactive
		IsVerified:  false,
	}
	if !repositories.IsPlatformScope(targetInstitution) {
		instID := targetInstitution
		user.InstitutionID = &instID
	}

	if err := uc.userRepo.Create(user); err != nil {
		logger.Error("Failed to create user: %v", err)
		c.JSON(http.StatusConflict, gin.H{"error": "Email already exists or creation failed"})
		return
	}

	uc.audit.Record(c, models.AuditUserCreate, "user", uintString(user.ID), user.Email, map[string]any{
		"assigned_role":  string(user.Role),
		"is_active":      user.IsActive,
		"institution_id": institutionDetail(user.InstitutionID),
	})

	logger.Info("User created successfully: %s (ID: %d, role %s)", user.Email, user.ID, user.Role)
	c.JSON(http.StatusCreated, gin.H{
		"message": "User created successfully",
		"user":    user,
	})
}

// UpdateUser updates an existing user
func (uc *UserController) UpdateUser(c *gin.Context) {
	inst := tenantID(c)

	id, err := parseIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get existing user, rejecting accounts outside the caller's institution.
	user, err := uc.userRepo.GetByID(id)
	if err != nil {
		respondRepoError(c, "User not found", err)
		return
	}
	if user.InstitutionID == nil || !repositories.InTenant(*user.InstitutionID, inst) {
		respondNotFound(c, "User not found")
		return
	}

	// Role changes go through the same policy as creation, and are audited
	// with the before/after pair so the trail shows what actually changed.
	//
	// An institution admin may only hand out the plain `user` role, which in
	// practice means they cannot promote anyone. Promotion is a platform
	// decision. This is deliberately stricter than the old rule, which let an
	// institution admin create other institution admins.
	currentUserRole := models.UserRole(c.GetString("role"))
	roleChanging := req.Role != "" && models.UserRole(req.Role) != user.Role
	previousRole := user.Role
	previousActive := user.IsActive

	if roleChanging {
		if !req.Role.IsValid() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role"})
			return
		}
		if !auth.CanAssign(currentUserRole, models.UserRole(req.Role)) {
			uc.audit.RecordDenied(c, models.AuditRoleChange, "user", uintString(user.ID), user.Email,
				"caller "+string(currentUserRole)+" may not assign role "+string(req.Role))
			c.JSON(http.StatusForbidden, gin.H{
				"error":          "You may not assign this role",
				"your_role":      string(currentUserRole),
				"assignable":     roleStrings(auth.AssignableRoles(currentUserRole)),
				"requested_role": string(req.Role),
			})
			return
		}
		if models.UserRole(req.Role) == models.RoleSupport {
			uc.audit.RecordDenied(c, models.AuditRoleChange, "user", uintString(user.ID), user.Email,
				"support role cannot be assigned to a persistent account")
			c.JSON(http.StatusForbidden, gin.H{"error": "This role cannot be assigned to an account"})
			return
		}
		// A super_admin role on an institution-bound account would be refused
		// by the workspace gate anyway; refuse it here with a clear message.
		if models.UserRole(req.Role) == models.RoleSuperAdmin && user.InstitutionID != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "A super_admin must not be bound to an institution",
				"details": "This user belongs to an institution and cannot hold the platform role.",
			})
			return
		}
		// Changing a super_admin's role is platform-only, and only from the
		// platform group. A super_admin has no institution, so it can never
		// appear in a tenant-scoped listing; this is defence in depth.
		if previousRole == models.RoleSuperAdmin && !currentUserRole.IsPlatformRole() {
			uc.audit.RecordDenied(c, models.AuditRoleChange, "user", uintString(user.ID), user.Email,
				"non-platform caller attempted to change a super_admin role")
			c.JSON(http.StatusForbidden, gin.H{"error": "Only a platform administrator can change a platform account"})
			return
		}
		user.Role = models.UserRole(req.Role)
	}

	// Update fields if provided
	if req.Email != "" {
		user.Email = req.Email
	}
	if req.FirstName != "" {
		user.FirstName = req.FirstName
	}
	if req.LastName != "" {
		user.LastName = req.LastName
	}
	if req.PhoneNumber != "" {
		user.PhoneNumber = req.PhoneNumber
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}

	if err := uc.userRepo.Update(inst, user); err != nil {
		logger.Error("Failed to update user %d: %v", id, err)
		respondRepoError(c, "User not found", err)
		return
	}

	// A role change and an activation change are separately auditable, because
	// "who was promoted" and "who was re-enabled" are different questions.
	if roleChanging {
		uc.audit.Record(c, models.AuditRoleChange, "user", uintString(user.ID), user.Email, map[string]any{
			"from": string(previousRole),
			"to":   string(user.Role),
		})
	}
	if user.IsActive != previousActive {
		action := models.AuditUserActivate
		if !user.IsActive {
			action = models.AuditUserSuspend
		}
		uc.audit.Record(c, action, "user", uintString(user.ID), user.Email, map[string]any{
			"is_active": user.IsActive,
		})
	}
	if roleChanging || user.IsActive != previousActive || req.Email != "" {
		uc.audit.Record(c, models.AuditUserUpdate, "user", uintString(user.ID), user.Email, map[string]any{
			"role":      string(user.Role),
			"is_active": user.IsActive,
			"email":     user.Email,
		})
	}

	logger.Info("User updated successfully: %d", id)
	c.JSON(http.StatusOK, gin.H{
		"message": "User updated successfully",
		"user":    user,
	})
}

// DeleteUser deletes a user
func (uc *UserController) DeleteUser(c *gin.Context) {
	inst := tenantID(c)

	id, err := parseIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	// Check the user exists and is in the caller's institution.
	user, err := uc.userRepo.GetByID(id)
	if err != nil {
		respondRepoError(c, "User not found", err)
		return
	}
	if user.InstitutionID == nil || !repositories.InTenant(*user.InstitutionID, inst) {
		respondNotFound(c, "User not found")
		return
	}

	// A platform account cannot be reached here: it has no institution, so the
	// tenant check above already rejected it. A support session holds no
	// delete permission and is refused by the route's RequirePermission, but
	// the role is checked again so the rule holds even if a route is wired
	// differently later.
	currentUserRole := models.UserRole(c.GetString("role"))
	if currentUserRole == models.RoleSupport {
		uc.audit.RecordDenied(c, models.AuditUserDelete, "user", uintString(id), user.Email,
			"support session attempted a delete")
		c.JSON(http.StatusForbidden, gin.H{"error": "A support session is read-only"})
		return
	}
	if user.Role == models.RoleSuperAdmin && !currentUserRole.IsPlatformRole() {
		uc.audit.RecordDenied(c, models.AuditUserDelete, "user", uintString(id), user.Email,
			"non-platform caller attempted to delete a platform account")
		c.JSON(http.StatusForbidden, gin.H{"error": "Only a platform administrator can delete a platform account"})
		return
	}

	// Prevent self-deletion
	if currentID, ok := currentUserID(c); ok && currentID == id {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete your own account"})
		return
	}

	deletedEmail := user.Email
	deletedRole := user.Role

	if err := uc.userRepo.Delete(inst, id); err != nil {
		logger.Error("Failed to delete user %d: %v", id, err)
		respondRepoError(c, "User not found", err)
		return
	}

	uc.audit.Record(c, models.AuditUserDelete, "user", uintString(id), deletedEmail, map[string]any{
		"role":     string(deletedRole),
		"self":     false,
		"severity": "high",
	})

	logger.Info("User deleted successfully: %d", id)
	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}

// ChangePassword allows a user to change their password. The acting user comes
// from the resolved session, not from the request.
func (uc *UserController) ChangePassword(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if strings.Contains(err.Error(), "NewPassword") || strings.Contains(err.Error(), "new_password") || strings.Contains(err.Error(), "Password") {
			c.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !security.ValidPassword(req.NewPassword) {
		c.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
		return
	}

	// Get current user
	user, err := uc.userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Failed to retrieve user %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user"})
		return
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.CurrentPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Current password is incorrect"})
		return
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("Failed to hash new password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process new password"})
		return
	}

	if err := uc.userRepo.UpdatePassword(userID, string(hashedPassword)); err != nil {
		logger.Error("Failed to update password for user %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	logger.Info("Password changed successfully for user %d", userID)
	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// GetMyPermissions handles GET /api/protected/me/permissions.
//
// The frontend calls this after login and uses the list to decide which nav
// items and buttons to render. Driving the UI from the server's own permission
// map — rather than hardcoding a role check in the frontend — is what keeps the
// two from disagreeing about what a role can do.
//
// The response also states the caller's scope, because a super_admin and an
// institution admin have genuinely different shapes of access and the UI
// needs to branch on that.
func (uc *UserController) GetMyPermissions(c *gin.Context) {
	role := models.UserRole(c.GetString("role"))
	inst := tenantID(c)
	isPlatform := role == models.RoleSuperAdmin && repositories.IsPlatformScope(inst)

	permissions := auth.PermissionsFor(role)
	asStrings := make([]string, 0, len(permissions))
	for _, p := range permissions {
		asStrings = append(asStrings, string(p))
	}

	body := gin.H{
		"role":                string(role),
		"permissions":         asStrings,
		"assignable_roles":    roleStrings(auth.AssignableRoles(role)),
		"institution_id":      nil,
		"is_platform_account": isPlatform,
		"is_support_session":  role == models.RoleSupport,
		"read_only":           auth.IsReadOnly(role),
	}
	if !repositories.IsPlatformScope(inst) {
		body["institution_id"] = inst
	}
	if role.IsPlatformRole() {
		// A platform account has no institution workspace, and the response
		// says so explicitly rather than letting the UI guess from a null id.
		body["workspace"] = "platform"
	} else if role == models.RoleSupport {
		body["workspace"] = "support"
	} else {
		body["workspace"] = "institution"
	}

	c.JSON(http.StatusOK, body)
}

// roleStrings renders a role list for JSON output.
func roleStrings(roles []models.UserRole) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, string(r))
	}
	return out
}

// uintString renders a uint ID for an audit target.
func uintString(id uint) string { return strconv.FormatUint(uint64(id), 10) }

// institutionDetail renders an institution pointer for an audit detail map,
// distinguishing "platform" from "no institution set" in the trail.
func institutionDetail(id *uint) any {
	if id == nil {
		return "platform"
	}
	return *id
}

// GetProfile returns the current user's profile
func (uc *UserController) GetProfile(c *gin.Context) {
	inst := tenantID(c)

	userID, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	user, err := uc.userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Failed to retrieve user profile %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve profile"})
		return
	}

	// Update last login time. A failure here must not fail the profile read,
	// so the error is logged and ignored.
	now := time.Now()
	user.LastLoginAt = &now
	if err := uc.userRepo.Update(inst, user); err != nil && err != gorm.ErrRecordNotFound {
		logger.Warn("Failed to update last login for user %d: %v", userID, err)
	}

	logger.Info("Profile retrieved for user %d", userID)
	c.JSON(http.StatusOK, gin.H{"user": user})
}
