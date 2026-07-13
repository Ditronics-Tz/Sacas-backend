package app

import (
	"fmt"
	"io"
	"log"
	"os"

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

	// Quiet mode: no Gin route-registration spam; only our request/error logs
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = os.Stderr

	db := database.InitDB()
	defer database.CloseDB(db)

	if err := database.RunMigrations(db); err != nil {
		return fmt.Errorf("database migrations: %w", err)
	}

	if err := database.CreateInitialData(db); err != nil {
		logger.Error("seed failed: %v", err)
	}

	redisClient := database.InitRedis()
	defer database.CloseRedis(redisClient)

	if _, err := config.ResolveJWTSecret(); err != nil {
		return fmt.Errorf("JWT configuration: %w", err)
	}

	// gin.New = no default Logger/Recovery; we add our own
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	router.Use(middlewares.RecoveryMiddleware())
	router.Use(middlewares.RequestLogger())
	router.Use(middlewares.MetricsMiddleware())

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
	// Short banner only — then pure request/error stream
	fmt.Fprintf(os.Stdout, "\n  SACAS API  %s\n  health     %s/api/health\n  logging    requests + errors only (OPTIONS hidden)\n  stop       Ctrl+C\n\n", base, base)
}
