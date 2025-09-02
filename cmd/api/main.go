package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"go_boilerplate/internal/config"
	"go_boilerplate/internal/controllers"
	"go_boilerplate/internal/database"
	"go_boilerplate/internal/middlewares"
	"go_boilerplate/internal/routes"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
)

func main() {
	var err error

	// Load environment variables
	err = godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Initialize logger
	logger.InitLogger()
	logger.Info("Starting Go Boilerplate API server...")

	// Initialize database
	db := database.InitDB()
	defer database.CloseDB(db)
	logger.Info("Database connection established")

	// Run database migrations
	if err := database.RunMigrations(db); err != nil {
		logger.Fatal("Failed to run database migrations: %v", err)
	}

	// Create initial data (super admin user)
	if err := database.CreateInitialData(db); err != nil {
		logger.Warn("Failed to create initial data: %v", err)
	}

	// Initialize Redis
	redisClient := database.InitRedis()
	defer database.CloseRedis(redisClient)
	logger.Info("Redis connection established")

	// Set Gin mode based on environment
	if config.GetEnv("ENV", "development") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// Global middlewares
	router.Use(middlewares.MetricsMiddleware())
	router.Use(middlewares.RecoveryMiddleware())

	// Initialize services and controllers
	notificationService := services.NewNotificationService()
	otpController := controllers.NewOTPController(notificationService, redisClient)

	// Setup routes
	routes.SetupRoutes(router, db, otpController, redisClient)

	port := config.GetEnv("PORT", "8080")
	logger.Info("Server starting on port %s", port)
	logger.Info("API Endpoints available:")
	logger.Info("  - POST /api/auth/register")
	logger.Info("  - POST /api/auth/login")
	logger.Info("  - POST /api/auth/verify-email")
	logger.Info("  - POST /api/auth/forgot-password")
	logger.Info("  - POST /api/auth/reset-password")
	logger.Info("  - GET  /api/protected/profile")
	logger.Info("  - GET  /api/protected/admin/dashboard")
	logger.Info("  - GET  /api/protected/superadmin/dashboard")

	err = router.Run(":" + port)
	if err != nil {
		logger.Fatal("Failed to start server: %v", err)
	}
}