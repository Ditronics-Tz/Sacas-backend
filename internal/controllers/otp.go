package controllers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"

	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/security"
)

type OTPController struct {
	NotificationService *services.NotificationService
	RedisClient         *redis.Client
	otpGuard            *services.OTPAttemptGuard
}

func NewOTPController(notificationService *services.NotificationService, redisClient *redis.Client, otpGuard *services.OTPAttemptGuard) *OTPController {
	return &OTPController{
		NotificationService: notificationService,
		RedisClient:         redisClient,
		otpGuard:            otpGuard,
	}
}

type OTPRequest struct {
	Email string `json:"email" binding:"email"`
	Phone string `json:"phone"`
}

// SendOTP sends a generic OTP. Note: the OTP value is never logged (see
// auth.go devOTPLogging) — it is only delivered via email/SMS.
func (oc *OTPController) SendOTP(c *gin.Context) {
	var req OTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if oc.RedisClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OTP store unavailable (start Redis)"})
		return
	}

	otp := services.GenerateOTP(6)
	// Store OTP in Redis with 5 minutes expiry
	if err := oc.RedisClient.Set(c, "otp:"+req.Email, otp, 5*time.Minute).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store OTP"})
		return
	}

	var err error
	if req.Email != "" {
		err = oc.NotificationService.SendEmailOTP(req.Email, otp)
	} else if req.Phone != "" {
		err = oc.NotificationService.SendSMSOTP(req.Phone, otp)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send OTP"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "OTP sent successfully"})
}

type VerifyOTPRequest struct {
	Email string `json:"email" binding:"required,email"`
	OTP   string `json:"otp" binding:"required,len=6"`
}

func (oc *OTPController) VerifyOTP(c *gin.Context) {
	const purpose = "otp"
	var req VerifyOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if oc.RedisClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OTP store unavailable (start Redis)"})
		return
	}

	// Per-account brute-force lockout (see AuthController.VerifyEmail).
	if oc.otpGuard.AttemptsExceeded(c, purpose, req.Email) {
		oc.RedisClient.Del(c, "otp:"+req.Email)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many failed attempts. Request a new OTP."})
		return
	}

	storedOTP, err := oc.RedisClient.Get(c, "otp:"+req.Email).Result()
	if err == redis.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OTP expired or not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve OTP"})
		return
	}

	if !security.SecureCompare(storedOTP, req.OTP) {
		ttl := services.RemainingTTL(c, oc.RedisClient, "otp:"+req.Email, 5*time.Minute)
		oc.otpGuard.RecordFailure(c, purpose, req.Email, ttl)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid OTP"})
		return
	}

	// OTP verified, delete it (and the attempt counter) from Redis
	if err := oc.RedisClient.Del(c, "otp:"+req.Email).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete OTP"})
		return
	}
	oc.otpGuard.Reset(c, purpose, req.Email)

	c.JSON(http.StatusOK, gin.H{"message": "OTP verified successfully"})
}
