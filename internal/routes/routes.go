package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"

	"go_boilerplate/internal/config"
	"go_boilerplate/internal/controllers"
	"go_boilerplate/internal/middlewares"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
)

func SetupRoutes(router *gin.Engine, db *gorm.DB, otpController *controllers.OTPController, redisClient *redis.Client) {
	// Initialize repositories and services
	userRepo := repositories.NewUserRepository(db)
	notificationService := services.NewNotificationService()
	
	// Initialize controllers
	authController := controllers.NewAuthController(userRepo, notificationService, redisClient)
	userController := controllers.NewUserController(userRepo)

	// Security middleware
	securityConfig := middlewares.DefaultSecurityConfig()
	router.Use(middlewares.SecurityMiddleware(securityConfig))

	// CSRF middleware (conditionally enabled)
	if config.GetEnv("CSRF_ENABLED", "true") == "true" {
		csrfConfig := middlewares.CSRFConfig{
			RedisClient: redisClient,
			SkipPaths: []string{
				"/api/health",
				"/api/metrics",
			},
		}
		router.Use(middlewares.CSRFMiddleware(csrfConfig))
	}

	api := router.Group("/api")
	{
		// Public endpoints
		api.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":    "ok",
				"timestamp": gin.H{"timestamp": "now"},
				"version":   "1.0.0",
			})
		})

		api.GET("/metrics", func(c *gin.Context) {
			if metrics, exists := c.Get("metrics"); exists {
				c.JSON(200, metrics)
			} else {
				c.JSON(200, gin.H{"message": "No metrics available"})
			}
		})

		// Authentication endpoints
		auth := api.Group("/auth")
		{
			auth.POST("/register", authController.Register)
			auth.POST("/login", authController.Login)
			auth.POST("/verify-email", authController.VerifyEmail)
			auth.POST("/forgot-password", authController.ForgotPassword)
			auth.POST("/reset-password", authController.ResetPassword)
			auth.POST("/resend-verification", authController.ResendVerificationOTP)
			auth.POST("/logout", authController.Logout)
		}

		// OTP endpoints
		otp := api.Group("/otp")
		{
			otp.POST("/send", otpController.SendOTP)
			otp.POST("/verify", otpController.VerifyOTP)
		}

		// Protected endpoints
		protected := api.Group("/protected")
		protected.Use(middlewares.JWTAuthMiddleware())
		{
			// User profile endpoints
			protected.GET("/profile", userController.GetProfile)
			protected.PUT("/change-password", userController.ChangePassword)

			// User endpoints (any authenticated user can view, but admin required for some operations)
			users := protected.Group("/users")
			{
				users.GET("", middlewares.AdminMiddleware(), userController.GetUsers)
				users.GET("/:id", middlewares.AdminMiddleware(), userController.GetUser)
				users.POST("", middlewares.AdminMiddleware(), userController.CreateUser)
				users.PUT("/:id", middlewares.AdminMiddleware(), userController.UpdateUser)
				users.DELETE("/:id", middlewares.AdminMiddleware(), userController.DeleteUser)
			}

			// Admin-only endpoints
			admin := protected.Group("/admin")
			admin.Use(middlewares.AdminMiddleware())
			{
				admin.GET("/dashboard", func(c *gin.Context) {
					userRole := c.GetString("role")
					c.JSON(200, gin.H{
						"message": "Welcome to admin dashboard",
						"role":    userRole,
						"features": []string{
							"User Management",
							"System Monitoring",
							"Reports",
						},
					})
				})

				admin.GET("/users/stats", func(c *gin.Context) {
					// Get user statistics
					var totalUsers, activeUsers, adminUsers int64
					db.Model(&models.User{}).Count(&totalUsers)
					db.Model(&models.User{}).Where("is_active = ?", true).Count(&activeUsers)
					db.Model(&models.User{}).Where("role = ? OR role = ?", "administrator", "super_admin").Count(&adminUsers)

					c.JSON(200, gin.H{
						"total_users":  totalUsers,
						"active_users": activeUsers,
						"admin_users":  adminUsers,
					})
				})
			}

			// Super Admin-only endpoints
			superadmin := protected.Group("/superadmin")
			superadmin.Use(middlewares.SuperAdminMiddleware())
			{
				superadmin.GET("/dashboard", func(c *gin.Context) {
					c.JSON(200, gin.H{
						"message": "Welcome to super admin dashboard",
						"features": []string{
							"Full System Access",
							"User Role Management",
							"System Configuration",
							"Advanced Analytics",
						},
					})
				})

				superadmin.GET("/system/info", func(c *gin.Context) {
					c.JSON(200, gin.H{
						"version":     "1.0.0",
						"environment": config.GetEnv("ENV", "development"),
						"database":    "PostgreSQL",
						"cache":       "Redis",
						"features": gin.H{
							"jwt_auth":        true,
							"otp_verification": true,
							"csrf_protection":  config.GetEnv("CSRF_ENABLED", "true") == "true",
							"rate_limiting":    config.GetEnv("RATE_LIMIT_ENABLED", "true") == "true",
						},
					})
				})
			}
		}
	}
}