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

	// Load environment variables (optional when running under Docker/K8s with real env)
	err = godotenv.Load()
	if err != nil {
		log.Println("No .env file found — using process environment")
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

	// Fail fast on weak JWT secrets in production
	jwtSecret, err := config.ResolveJWTSecret()
	if err != nil {
		logger.Fatal("JWT configuration error: %v", err)
	}
	if config.IsWeakJWTSecret(jwtSecret) {
		logger.Warn("JWT_SECRET is a weak/dev default — do not use in production")
	}

	// CSRF posture is applied inside SetupRoutes; log here for visibility at boot
	csrfState := config.GetEnv("CSRF_ENABLED", "false")
	if csrfState == "true" {
		logger.Info("CSRF_ENABLED=true — mutating requests require X-CSRF-Token (Redis-backed)")
	} else {
		logger.Info("CSRF_ENABLED=false — CSRF checks disabled (SPA dev mode)")
	}
	logger.Info("CORS_ALLOWED_ORIGINS=%s", config.GetEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173"))
	solverURL := config.GetEnv("SOLVER_URL", "")
	if solverURL == "" {
		logger.Info("SOLVER_URL=(empty) — greedy engine only")
	} else {
		logger.Info("SOLVER_URL=%s SOLVER_FALLBACK=%s", solverURL, config.GetEnv("SOLVER_FALLBACK", "false"))
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
	logger.Info("Timetable System Endpoints:")
	logger.Info("  - POST /api/protected/timetable/faculties")
	logger.Info("  - GET  /api/protected/timetable/faculties")
	logger.Info("  - POST /api/protected/timetable/staff")
	logger.Info("  - GET  /api/protected/timetable/staff")
	logger.Info("  - POST /api/protected/timetable/generate")
	logger.Info("  - POST /api/protected/timetable/generate/preview")
	logger.Info("  - GET  /api/protected/timetable/class/:class_id")
	logger.Info("  - GET  /api/protected/timetable/by-staff/:staff_id")

	err = router.Run(":" + port)
	if err != nil {
		logger.Fatal("Failed to start server: %v", err)
	}
}