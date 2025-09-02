package controllers

import (
	"net/http"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"golang.org/x/crypto/bcrypt"

	"go_boilerplate/internal/config"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
	"gorm.io/gorm"
)

type AuthController struct {
	userRepo            repositories.UserRepository
	notificationService *services.NotificationService
	redisClient         *redis.Client
}

func NewAuthController(userRepo repositories.UserRepository, notificationService *services.NotificationService, redisClient *redis.Client) *AuthController {
	return &AuthController{
		userRepo:            userRepo,
		notificationService: notificationService,
		redisClient:         redisClient,
	}
}

type RegisterRequest struct {
	Email       string          `json:"email" binding:"required,email"`
	Password    string          `json:"password" binding:"required,min=6"`
	FirstName   string          `json:"first_name" binding:"required,min=2,max=50"`
	LastName    string          `json:"last_name" binding:"required,min=2,max=50"`
	PhoneNumber string          `json:"phone_number,omitempty"`
}

func (ac *AuthController) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid registration request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user already exists
	existingUser, err := ac.userRepo.GetByEmail(req.Email)
	if err == nil && existingUser != nil {
		logger.Warn("Registration attempt with existing email: %s", req.Email)
		c.JSON(http.StatusConflict, gin.H{"error": "Email already exists"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("Failed to hash password during registration: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	user := &models.User{
		Email:       req.Email,
		Password:    string(hashedPassword),
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		PhoneNumber: req.PhoneNumber,
		Role:        models.RoleUser, // Default role
		IsActive:    false,           // Requires activation
		IsVerified:  false,           // Requires verification
	}

	if err := ac.userRepo.Create(user); err != nil {
		logger.Error("Failed to create user during registration: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Send verification OTP
	otp := services.GenerateOTP(6)
	if err := ac.redisClient.Set(c, "verify:"+req.Email, otp, 15*time.Minute).Err(); err != nil {
		logger.Error("Failed to store verification OTP: %v", err)
	} else {
		if err := ac.notificationService.SendEmailOTP(req.Email, otp); err != nil {
			logger.Error("Failed to send verification email: %v", err)
		}
	}

	logger.Info("User registered successfully: %s (ID: %d)", user.Email, user.ID)
	c.JSON(http.StatusCreated, gin.H{
		"message": "User registered successfully. Please check your email for verification code.",
		"user_id": user.ID,
	})
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (ac *AuthController) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid login request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := ac.userRepo.GetByEmail(req.Email)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			logger.Warn("Login attempt with non-existent email: %s", req.Email)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		} else {
			logger.Error("Database error during login: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		}
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		logger.Warn("Invalid password attempt for user: %s", req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if !user.IsActive {
		logger.Warn("Login attempt by inactive user: %s", req.Email)
		c.JSON(http.StatusForbidden, gin.H{"error": "Account is not active"})
		return
	}

	// Update last login time
	now := time.Now()
	user.LastLoginAt = &now
	ac.userRepo.Update(user)

	// Generate JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"role":    string(user.Role),
		"email":   user.Email,
		"exp":     time.Now().Add(time.Hour * 24).Unix(),
		"iat":     time.Now().Unix(),
	})

	tokenString, err := token.SignedString([]byte(config.GetEnv("JWT_SECRET", "mysecretkey")))
	if err != nil {
		logger.Error("Failed to generate JWT token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// Set secure HTTP-only cookie
	secure := config.GetEnv("ENV", "development") == "production"
	c.SetCookie("token", tokenString, 24*3600, "/", "", secure, true)

	logger.Info("User logged in successfully: %s (ID: %d)", user.Email, user.ID)
	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   tokenString,
		"user": gin.H{
			"id":         user.ID,
			"email":      user.Email,
			"first_name": user.FirstName,
			"last_name":  user.LastName,
			"role":       user.Role,
			"is_active":  user.IsActive,
		},
	})
}

// VerifyEmailRequest represents the request for email verification
type VerifyEmailRequest struct {
	Email string `json:"email" binding:"required,email"`
	OTP   string `json:"otp" binding:"required,len=6"`
}

// VerifyEmail verifies user's email address with OTP
func (ac *AuthController) VerifyEmail(c *gin.Context) {
	var req VerifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify OTP
	storedOTP, err := ac.redisClient.Get(c, "verify:"+req.Email).Result()
	if err == redis.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OTP expired or not found"})
		return
	} else if err != nil {
		logger.Error("Failed to retrieve verification OTP: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify OTP"})
		return
	}

	if storedOTP != req.OTP {
		logger.Warn("Invalid verification OTP for email: %s", req.Email)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid OTP"})
		return
	}

	// Get and update user
	user, err := ac.userRepo.GetByEmail(req.Email)
	if err != nil {
		logger.Error("User not found during verification: %s", req.Email)
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	user.IsVerified = true
	user.IsActive = true // Activate user upon verification
	if err := ac.userRepo.Update(user); err != nil {
		logger.Error("Failed to update user verification status: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify email"})
		return
	}

	// Delete OTP from Redis
	ac.redisClient.Del(c, "verify:"+req.Email)

	logger.Info("Email verified successfully for user: %s", req.Email)
	c.JSON(http.StatusOK, gin.H{"message": "Email verified successfully"})
}

// ForgotPasswordRequest represents the request for password reset
type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// ForgotPassword initiates password reset process
func (ac *AuthController) ForgotPassword(c *gin.Context) {
	var req ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user exists
	user, err := ac.userRepo.GetByEmail(req.Email)
	if err != nil {
		// Don't reveal if email exists or not for security
		c.JSON(http.StatusOK, gin.H{"message": "If the email exists, a password reset code has been sent"})
		return
	}

	if !user.IsActive {
		c.JSON(http.StatusOK, gin.H{"message": "If the email exists, a password reset code has been sent"})
		return
	}

	// Generate and store reset OTP
	otp := services.GenerateOTP(6)
	if err := ac.redisClient.Set(c, "reset:"+req.Email, otp, 15*time.Minute).Err(); err != nil {
		logger.Error("Failed to store password reset OTP: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initiate password reset"})
		return
	}

	// Send reset email
	if err := ac.notificationService.SendEmailOTP(req.Email, otp); err != nil {
		logger.Error("Failed to send password reset email: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send reset email"})
		return
	}

	logger.Info("Password reset initiated for user: %s", req.Email)
	c.JSON(http.StatusOK, gin.H{"message": "If the email exists, a password reset code has been sent"})
}

// ResetPasswordRequest represents the request for password reset
type ResetPasswordRequest struct {
	Email       string `json:"email" binding:"required,email"`
	OTP         string `json:"otp" binding:"required,len=6"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// ResetPassword resets user password with OTP
func (ac *AuthController) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify OTP
	storedOTP, err := ac.redisClient.Get(c, "reset:"+req.Email).Result()
	if err == redis.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OTP expired or not found"})
		return
	} else if err != nil {
		logger.Error("Failed to retrieve password reset OTP: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify OTP"})
		return
	}

	if storedOTP != req.OTP {
		logger.Warn("Invalid password reset OTP for email: %s", req.Email)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid OTP"})
		return
	}

	// Get user
	user, err := ac.userRepo.GetByEmail(req.Email)
	if err != nil {
		logger.Error("User not found during password reset: %s", req.Email)
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("Failed to hash new password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process new password"})
		return
	}

	// Update password
	if err := ac.userRepo.UpdatePassword(user.ID, string(hashedPassword)); err != nil {
		logger.Error("Failed to update password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}

	// Delete OTP from Redis
	ac.redisClient.Del(c, "reset:"+req.Email)

	logger.Info("Password reset successfully for user: %s", req.Email)
	c.JSON(http.StatusOK, gin.H{"message": "Password reset successfully"})
}

// Logout clears the authentication cookie
func (ac *AuthController) Logout(c *gin.Context) {
	// Clear the cookie
	c.SetCookie("token", "", -1, "/", "", false, true)
	
	logger.Info("User logged out successfully")
	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// ResendVerificationOTP resends verification OTP
func (ac *AuthController) ResendVerificationOTP(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user exists and is not already verified
	user, err := ac.userRepo.GetByEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if user.IsVerified {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email already verified"})
		return
	}

	// Generate and store new OTP
	otp := services.GenerateOTP(6)
	if err := ac.redisClient.Set(c, "verify:"+req.Email, otp, 15*time.Minute).Err(); err != nil {
		logger.Error("Failed to store verification OTP: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate OTP"})
		return
	}

	// Send verification email
	if err := ac.notificationService.SendEmailOTP(req.Email, otp); err != nil {
		logger.Error("Failed to send verification email: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send verification email"})
		return
	}

	logger.Info("Verification OTP resent for user: %s", req.Email)
	c.JSON(http.StatusOK, gin.H{"message": "Verification code resent successfully"})
}