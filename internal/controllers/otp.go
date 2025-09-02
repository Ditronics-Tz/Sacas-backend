package controllers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"

	"go_boilerplate/internal/services"
)

type OTPController struct {
	NotificationService *services.NotificationService
	RedisClient         *redis.Client
}

func NewOTPController(notificationService *services.NotificationService, redisClient *redis.Client) *OTPController {
	return &OTPController{
		NotificationService: notificationService,
		RedisClient:         redisClient,
	}
}

type OTPRequest struct {
	Email string `json:"email" binding:"email"`
	Phone string `json:"phone"`
}

func (oc *OTPController) SendOTP(c *gin.Context) {
	var req OTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	var req VerifyOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

	if storedOTP != req.OTP {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid OTP"})
		return
	}

	// OTP verified, delete it from Redis
	if err := oc.RedisClient.Del(c, "otp:"+req.Email).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete OTP"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "OTP verified successfully"})
}