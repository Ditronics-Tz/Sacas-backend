package app

import (
	"fmt"
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

// Run boots the SACAS API (used by `go run .` and `go run ./cmd/api`).
func Run() error {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found — using process environment")
	}

	logger.InitLogger()
	logger.Info("Starting SACAS API (dev server)...")

	db := database.InitDB()
	defer database.CloseDB(db)
	logger.Info("Database connection established")

	if err := database.RunMigrations(db); err != nil {
		return fmt.Errorf("database migrations: %w", err)
	}

	if err := database.CreateInitialData(db); err != nil {
		logger.Warn("Failed to create initial data: %v", err)
	}

	redisClient := database.InitRedis()
	defer database.CloseRedis(redisClient)
	if redisClient != nil {
		logger.Info("Redis connection established")
	} else {
		logger.Warn("Redis unavailable — running without OTP store / rate-limit backend (dev OK)")
	}

	if config.GetEnv("ENV", "development") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	jwtSecret, err := config.ResolveJWTSecret()
	if err != nil {
		return fmt.Errorf("JWT configuration: %w", err)
	}
	if config.IsWeakJWTSecret(jwtSecret) {
		logger.Warn("JWT_SECRET is a weak/dev default — do not use in production")
	}

	csrfState := config.GetEnv("CSRF_ENABLED", "false")
	if csrfState == "true" {
		logger.Info("CSRF_ENABLED=true — mutating requests require X-CSRF-Token")
	} else {
		logger.Info("CSRF_ENABLED=false — CSRF checks disabled (SPA dev mode)")
	}
	logger.Info("CORS_ALLOWED_ORIGINS=%s", config.GetEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173"))

	solverURL := config.GetEnv("SOLVER_URL", "")
	if solverURL == "" {
		logger.Info("SOLVER_URL=(empty) — using built-in greedy timetable engine")
	} else {
		logger.Info("SOLVER_URL=%s SOLVER_FALLBACK=%s", solverURL, config.GetEnv("SOLVER_FALLBACK", "false"))
	}

	router := gin.Default()
	router.Use(middlewares.MetricsMiddleware())
	router.Use(middlewares.RecoveryMiddleware())

	notificationService := services.NewNotificationService()
	otpController := controllers.NewOTPController(notificationService, redisClient)
	routes.SetupRoutes(router, db, otpController, redisClient)

	port := config.GetEnv("PORT", "8080")
	printDevBanner(port)

	if err := router.Run(":" + port); err != nil {
		return fmt.Errorf("server listen: %w", err)
	}
	return nil
}

func printDevBanner(port string) {
	base := "http://localhost:" + port
	logger.Info("========================================================")
	logger.Info("  SACAS API is running")
	logger.Info("  Base URL : %s", base)
	logger.Info("  Health   : %s/api/health", base)
	logger.Info("  Login    : POST %s/api/auth/login", base)
	logger.Info("  Admin    : admin@example.com / password")
	logger.Info("========================================================")
	logger.Info("Quick try (PowerShell):")
	logger.Info("  Invoke-RestMethod %s/api/health", base)
	logger.Info("  Invoke-RestMethod -Method POST %s/api/auth/login -ContentType application/json -Body '{\"email\":\"admin@example.com\",\"password\":\"password\"}'", base)
	logger.Info("Stop server: Ctrl+C")
}
