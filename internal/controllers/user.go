package controllers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
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
	Password    string          `json:"password" binding:"required,min=6"`
	FirstName   string          `json:"first_name" binding:"required,min=2,max=50"`
	LastName    string          `json:"last_name" binding:"required,min=2,max=50"`
	PhoneNumber string          `json:"phone_number,omitempty"`
	Role        models.UserRole `json:"role" binding:"required"`
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
	NewPassword     string `json:"new_password" binding:"required,min=6"`
}

// GetUsers retrieves a paginated list of users
func (uc *UserController) GetUsers(c *gin.Context) {
	// Get pagination parameters
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
		users, err = uc.userRepo.GetByRole(role, limit, offset)
	} else {
		users, err = uc.userRepo.GetAll(limit, offset)
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

// GetUser retrieves a single user by ID
func (uc *UserController) GetUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	user, err := uc.userRepo.GetByID(uint(id))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		} else {
			logger.Error("Failed to retrieve user %d: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user"})
		}
		return
	}

	logger.Info("Retrieved user %d", id)
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// CreateUser creates a new user (admin only)
func (uc *UserController) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get existing user
	user, err := uc.userRepo.GetByID(uint(id))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		} else {
			logger.Error("Failed to retrieve user %d: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user"})
		}
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

	if err := uc.userRepo.Update(user); err != nil {
		logger.Error("Failed to update user %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user"})
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
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	// Check if user exists and get their role
	user, err := uc.userRepo.GetByID(uint(id))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		} else {
			logger.Error("Failed to retrieve user %d: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user"})
		}
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
	currentUserID := c.GetFloat64("user_id")
	if uint(currentUserID) == uint(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete your own account"})
		return
	}

	if err := uc.userRepo.Delete(uint(id)); err != nil {
		logger.Error("Failed to delete user %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
		return
	}

	logger.Info("User deleted successfully: %d", id)
	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}

// ChangePassword allows a user to change their password
func (uc *UserController) ChangePassword(c *gin.Context) {
	userID := uint(c.GetFloat64("user_id"))

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	userID := uint(c.GetFloat64("user_id"))

	user, err := uc.userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Failed to retrieve user profile %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve profile"})
		return
	}

	// Update last login time
	now := time.Now()
	user.LastLoginAt = &now
	uc.userRepo.Update(user)

	logger.Info("Profile retrieved for user %d", userID)
	c.JSON(http.StatusOK, gin.H{"user": user})
}