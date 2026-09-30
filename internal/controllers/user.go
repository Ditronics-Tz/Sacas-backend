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
	"go_boilerplate/pkg/logger"
	"go_boilerplate/pkg/security"
)

type UserController struct {
	userRepo repositories.UserRepository
}

func NewUserController(userRepo repositories.UserRepository) *UserController {
	return &UserController{
		userRepo: userRepo,
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

	// Check if user creating this has permission to assign the role
	currentUserRole := c.GetString("role")
	if currentUserRole != string(models.RoleSuperAdmin) {
		if req.Role == models.RoleSuperAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only super admin can create super admin users"})
			return
		}
		if req.Role == models.RoleAdmin && currentUserRole != string(models.RoleAdmin) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to create admin users"})
			return
		}
	}

	// The target institution is the caller's own, unless a platform
	// super_admin explicitly names another one.
	targetInstitution := inst
	isPlatform := currentUserRole == string(models.RoleSuperAdmin) && repositories.IsPlatformScope(inst)
	if isPlatform {
		if req.InstitutionID != nil {
			targetInstitution = *req.InstitutionID
		} else {
			// A platform super_admin with no explicit target creates a
			// platform-level account (institution_id NULL).
			targetInstitution = repositories.PlatformScope
		}
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

	logger.Info("User created successfully: %s (ID: %d)", user.Email, user.ID)
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

	// Check permissions for role changes
	currentUserRole := c.GetString("role")
	if req.Role != "" && req.Role != user.Role {
		if currentUserRole != string(models.RoleSuperAdmin) {
			if req.Role == models.RoleSuperAdmin || user.Role == models.RoleSuperAdmin {
				c.JSON(http.StatusForbidden, gin.H{"error": "Only super admin can modify super admin roles"})
				return
			}
			if (req.Role == models.RoleAdmin || user.Role == models.RoleAdmin) && currentUserRole != string(models.RoleAdmin) {
				c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to modify admin roles"})
				return
			}
		}
		if req.Role.IsValid() {
			user.Role = req.Role
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role"})
			return
		}
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

	// Check permissions
	currentUserRole := c.GetString("role")
	if currentUserRole != string(models.RoleSuperAdmin) {
		if user.Role == models.RoleSuperAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only super admin can delete super admin users"})
			return
		}
		if user.Role == models.RoleAdmin && currentUserRole != string(models.RoleAdmin) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to delete admin users"})
			return
		}
	}

	// Prevent self-deletion
	if currentID, ok := currentUserID(c); ok && currentID == id {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete your own account"})
		return
	}

	if err := uc.userRepo.Delete(inst, id); err != nil {
		logger.Error("Failed to delete user %d: %v", id, err)
		respondRepoError(c, "User not found", err)
		return
	}

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
