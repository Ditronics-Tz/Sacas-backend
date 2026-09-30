package routes

import (
	"context"
	"fmt"
	"net/http"
	"time"

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
	// CORS first so preflight and all responses get headers
	router.Use(middlewares.CORSMiddleware(middlewares.DefaultCORSConfig()))

	// Initialize repositories and services
	userRepo := repositories.NewUserRepository(db)
	facultyRepo := repositories.NewFacultyRepository(db)
	staffRepo := repositories.NewStaffRepository(db)
	courseRepo := repositories.NewCourseRepository(db)
	moduleRepo := repositories.NewModuleRepository(db)
	classRepo := repositories.NewClassRepository(db)
	roomRepo := repositories.NewRoomRepository(db)
	subjectRepo := repositories.NewSubjectRepository(db)
	timetableRepo := repositories.NewTimetableRepository(db)
	generationSettingsRepo := repositories.NewGenerationSettingsRepository(db)
	institutionRepo := repositories.NewInstitutionRepository(db)

	notificationService, err := services.NewNotificationService()
	if err != nil {
		// Production guard already handled in app.Run; dev fallback to log provider
		// Must panic here so misconfig is visible at boot, not silent
		panic(fmt.Sprintf("notification config: %v", err))
	}
	_ = notificationService // keep for authController below
	otpGuard := services.NewOTPAttemptGuard(redisClient)
	solverClient := services.NewSolverClient()
	timetableService := services.NewTimetableService(timetableRepo, staffRepo, classRepo, moduleRepo, roomRepo, subjectRepo, solverClient, generationSettingsRepo)

	// Initialize controllers
	authController := controllers.NewAuthController(userRepo, notificationService, redisClient, otpGuard)
	userController := controllers.NewUserController(userRepo)
	facultyController := controllers.NewFacultyController(facultyRepo)
	staffController := controllers.NewStaffControllerWithUser(staffRepo, moduleRepo, userRepo)
	courseController := controllers.NewCourseController(courseRepo)
	moduleController := controllers.NewModuleController(moduleRepo, courseRepo)
	classController := controllers.NewClassController(classRepo)
	roomController := controllers.NewRoomController(roomRepo)
	subjectController := controllers.NewSubjectController(subjectRepo)
	timetableController := controllers.NewTimetableController(timetableRepo, staffRepo, classRepo, roomRepo, timetableService)
	generationSettingsController := controllers.NewGenerationSettingsController(generationSettingsRepo)
	institutionController := controllers.NewInstitutionController(institutionRepo)

	// Security middleware
	securityConfig := middlewares.DefaultSecurityConfig()
	router.Use(middlewares.SecurityMiddleware(securityConfig))

	// CSRF middleware — default OFF for SPA local dev; set CSRF_ENABLED=true in production
	csrfEnabled := config.GetEnv("CSRF_ENABLED", "false") == "true"
	if csrfEnabled {
		// CSRF on — no startup spam
		csrfConfig := middlewares.CSRFConfig{
			RedisClient: redisClient,
			SkipPaths: []string{
				"/api/health",
			},
		}
		router.Use(middlewares.CSRFMiddleware(csrfConfig))
	} else {
		// CSRF off for SPA local dev
	}

	api := router.Group("/api")
	{
		// CSRF bootstrap for SPA (issues token when CSRF middleware is on; no-op message when off)
		api.GET("/csrf", func(c *gin.Context) {
			if config.GetEnv("CSRF_ENABLED", "false") != "true" {
				c.JSON(http.StatusOK, gin.H{
					"csrf_enabled": false,
					"message":      "CSRF protection is disabled",
				})
				return
			}
			// When CSRF middleware is active, token was already issued on this GET.
			token := c.Writer.Header().Get(middlewares.CSRFHeaderName)
			if token == "" {
				// Middleware not applied or issue failed — try explicit issue
				t, err := middlewares.IssueCSRFToken(c, redisClient)
				if err != nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{"error": "CSRF store unavailable", "csrf_enabled": true})
					return
				}
				token = t
			}
			c.JSON(http.StatusOK, gin.H{
				"csrf_enabled": true,
				"csrf_token":   token,
				"message":      "CSRF token issued; send as X-CSRF-Token on mutating requests",
			})
		})

		// Health with dependency checks
		api.GET("/health", func(c *gin.Context) {
			dbStatus := "up"
			redisStatus := "up"
			overall := "ok"

			sqlDB, err := db.DB()
			if err != nil || sqlDB.Ping() != nil {
				dbStatus = "down"
				overall = "degraded"
			}

			if redisClient == nil {
				redisStatus = "down"
				overall = "degraded"
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := redisClient.Ping(ctx).Err(); err != nil {
					redisStatus = "down"
					overall = "degraded"
				}
			}

			statusCode := http.StatusOK
			if overall != "ok" {
				statusCode = http.StatusServiceUnavailable
			}

			c.JSON(statusCode, gin.H{
				"status":    overall,
				"db":        dbStatus,
				"redis":     redisStatus,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"version":   "1.0.0",
			})
		})

		// Authentication endpoints (rate-limited)
		auth := api.Group("/auth")
		auth.Use(middlewares.RateLimitMiddleware(redisClient))
		{
			auth.POST("/register", authController.Register)
			auth.POST("/login", authController.Login)
			auth.POST("/verify-email", authController.VerifyEmail)
			auth.POST("/forgot-password", authController.ForgotPassword)
			auth.POST("/reset-password", authController.ResetPassword)
			auth.POST("/resend-verification", authController.ResendVerificationOTP)
			auth.POST("/logout", authController.Logout)
		}

		// OTP endpoints (rate-limited)
		otp := api.Group("/otp")
		otp.Use(middlewares.RateLimitMiddleware(redisClient))
		{
			otp.POST("/send", otpController.SendOTP)
			otp.POST("/verify", otpController.VerifyOTP)
		}

		// Protected endpoints
		//
		// Middleware order matters:
		//   JWTAuthMiddleware  — verify the token signature, set the claims
		//   TenantMiddleware   — load the user row and OVERWRITE user_id, role,
		//                        and email from the database, then resolve and
		//                        pin institution_id
		//
		// After this chain the identity and the tenant in the context are
		// authoritative. A role change, a deactivation, or a suspension takes
		// effect on the very next request rather than at token expiry, and no
		// handler can be tricked into another tenant via body, header, or a
		// stale token. TenantMiddleware also enforces the IsActive check that
		// the old ActiveUserMiddleware did, so it replaces that middleware
		// rather than being stacked with it.
		protected := api.Group("/protected")
		protected.Use(middlewares.JWTAuthMiddleware())
		protected.Use(middlewares.TenantMiddleware(userRepo.GetByID, institutionRepo.GetByID))
		{
			protected.GET("/profile", userController.GetProfile)
			protected.PUT("/change-password", userController.ChangePassword)

			// The signed-in tenant's own profile. The institution ID comes from
			// the resolved session, never from the request.
			protected.GET("/institution/me", institutionController.GetMe)

			// Authenticated staff member's own Staff profile. Resolved from the
			// session user_id via staff.user_id FK, scoped to the caller's
			// institution; returns 404 if no linked staff.
			protected.GET("/me/staff", staffController.GetMyStaff)

			// Authenticated staff member's own timetable. Resolved strictly from
			// the session user (Staff.user_id FK) within the caller's
			// institution; client-supplied staff IDs are ignored, so a
			// role=user account can only ever read its own timetable.
			// Registered before the admin-only /timetable group.
			protected.GET("/timetable/my", timetableController.GetMyTimetable)

			users := protected.Group("/users")
			{
				users.GET("", middlewares.AdminMiddleware(), userController.GetUsers)
				users.GET("/:id", middlewares.AdminMiddleware(), userController.GetUser)
				users.POST("", middlewares.AdminMiddleware(), userController.CreateUser)
				users.PUT("/:id", middlewares.AdminMiddleware(), userController.UpdateUser)
				users.DELETE("/:id", middlewares.AdminMiddleware(), userController.DeleteUser)
			}

			admin := protected.Group("/admin")
			admin.Use(middlewares.AdminMiddleware())
			{
				// Dashboard counts are scoped to the caller's institution so one
				// campus's admin never sees another campus's totals. A platform
				// super_admin (institution_id NULL) gets platform-wide counts.
				admin.GET("/dashboard", func(c *gin.Context) {
					inst := middlewares.InstitutionIDFromContext(c)

					var faculties, courses, modules, classes, rooms, staff, timetables int64
					repositories.TenantScope(db.Model(&models.Faculty{}), inst).Count(&faculties)
					repositories.TenantScope(db.Model(&models.Course{}), inst).Count(&courses)
					repositories.TenantScope(db.Model(&models.Module{}), inst).Count(&modules)
					repositories.TenantScope(db.Model(&models.Class{}), inst).Count(&classes)
					repositories.TenantScope(db.Model(&models.Room{}), inst).Count(&rooms)
					repositories.TenantScope(db.Model(&models.Staff{}), inst).Count(&staff)
					repositories.TenantScope(db.Model(&models.Timetable{}), inst).Count(&timetables)

					body := gin.H{
						"message": "Welcome to admin dashboard",
						"role":    c.GetString("role"),
						"counts": gin.H{
							"faculties":  faculties,
							"courses":    courses,
							"modules":    modules,
							"classes":    classes,
							"rooms":      rooms,
							"staff":      staff,
							"timetables": timetables,
						},
						"features": []string{
							"User Management",
							"System Monitoring",
							"Reports",
							"Timetable Generation",
						},
					}
					if repositories.IsPlatformScope(inst) {
						body["scope"] = "platform"
					} else {
						body["institution_id"] = inst
					}
					c.JSON(200, body)
				})

				// User stats are scoped to the caller's institution. Users with a
				// NULL institution_id are platform super_admins and are counted
				// only for platform callers.
				admin.GET("/users/stats", func(c *gin.Context) {
					inst := middlewares.InstitutionIDFromContext(c)

					base := func() *gorm.DB {
						q := db.Model(&models.User{})
						if repositories.IsPlatformScope(inst) {
							return q
						}
						return q.Where("institution_id = ?", inst)
					}

					var totalUsers, activeUsers, adminUsers int64
					base().Count(&totalUsers)
					base().Where("is_active = ?", true).Count(&activeUsers)
					base().Where("role = ? OR role = ?", "administrator", "super_admin").Count(&adminUsers)

					c.JSON(200, gin.H{
						"total_users":  totalUsers,
						"active_users": activeUsers,
						"admin_users":  adminUsers,
					})
				})

				// Metrics — previously public; now admin-only (authenticated)
				admin.GET("/metrics", func(c *gin.Context) {
					if metrics, exists := c.Get("metrics"); exists {
						c.JSON(200, metrics)
					} else {
						c.JSON(200, gin.H{"message": "No metrics available"})
					}
				})
			}

			superadmin := protected.Group("/superadmin")
			superadmin.Use(middlewares.SuperAdminMiddleware())
			{
				superadmin.GET("/dashboard", func(c *gin.Context) {
					totalInstitutions, _ := institutionRepo.CountAll()
					activeInstitutions, _ := institutionRepo.CountByStatus(models.InstitutionStatusActive)
					pendingInstitutions, _ := institutionRepo.CountByStatus(models.InstitutionStatusPending)
					suspendedInstitutions, _ := institutionRepo.CountByStatus(models.InstitutionStatusSuspended)

					c.JSON(200, gin.H{
						"message": "Welcome to super admin dashboard",
						"institutions": gin.H{
							"total":     totalInstitutions,
							"active":    activeInstitutions,
							"pending":   pendingInstitutions,
							"suspended": suspendedInstitutions,
						},
						"features": []string{
							"Full System Access",
							"User Role Management",
							"System Configuration",
							"Institution Management",
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
							"jwt_auth":         true,
							"otp_verification": true,
							"csrf_protection":  config.GetEnv("CSRF_ENABLED", "false") == "true",
							"rate_limiting":    config.GetEnv("RATE_LIMIT_ENABLED", "true") == "true",
							"solver":           config.GetEnv("SOLVER_URL", "") != "",
							"multi_tenancy":    true,
						},
					})
				})

				// Generation settings: the platform default solver knobs, plus
				// optional per-institution overrides. A super_admin may pass
				// ?institution_id= (or institution_id in the PUT body) to target
				// a specific tenant's override.
				superadmin.GET("/generation-settings", generationSettingsController.Get)
				superadmin.PUT("/generation-settings", generationSettingsController.Update)

				// Institution management. Gated again by RequirePlatformOnly so
				// a super_admin who HAS been assigned to an institution (and so
				// resolved to a tenant scope) cannot manage the tenant list.
				institutions := superadmin.Group("/institutions")
				institutions.Use(middlewares.RequirePlatformOnly())
				{
					institutions.POST("", institutionController.Create)
					institutions.GET("", institutionController.GetAll)
					institutions.GET("/:id", institutionController.Get)
					institutions.PUT("/:id", institutionController.Update)
					institutions.DELETE("/:id", institutionController.Delete)
				}
			}

			// Timetable Management endpoints (Admin access required)
			timetable := protected.Group("/timetable")
			timetable.Use(middlewares.AdminMiddleware())
			{
				// Faculty
				timetable.POST("/faculties", facultyController.CreateFaculty)
				timetable.GET("/faculties", facultyController.GetAllFaculties)
				timetable.GET("/faculties/:id", facultyController.GetFaculty)
				timetable.PUT("/faculties/:id", facultyController.UpdateFaculty)
				timetable.DELETE("/faculties/:id", facultyController.DeleteFaculty)

				// Course
				timetable.POST("/courses", courseController.CreateCourse)
				timetable.GET("/courses", courseController.GetAllCourses)
				timetable.GET("/courses/:id", courseController.GetCourse)
				timetable.PUT("/courses/:id", courseController.UpdateCourse)
				timetable.DELETE("/courses/:id", courseController.DeleteCourse)

				// Module
				timetable.POST("/modules", moduleController.CreateModule)
				timetable.GET("/modules", moduleController.GetAllModules)
				timetable.GET("/modules/:id", moduleController.GetModule)
				timetable.PUT("/modules/:id", moduleController.UpdateModule)
				timetable.DELETE("/modules/:id", moduleController.DeleteModule)
				// Use :id (same wildcard name as other /modules/:id routes — Gin requirement)
				timetable.GET("/modules/:id/staff", staffController.ListModuleStaff)

				// Class
				timetable.POST("/classes", classController.CreateClass)
				timetable.GET("/classes", classController.GetAllClasses)
				timetable.GET("/classes/:id", classController.GetClass)
				timetable.PUT("/classes/:id", classController.UpdateClass)
				timetable.DELETE("/classes/:id", classController.DeleteClass)

				// Room
				timetable.POST("/rooms", roomController.CreateRoom)
				timetable.GET("/rooms", roomController.GetAllRooms)
				timetable.GET("/rooms/:id", roomController.GetRoom)
				timetable.PUT("/rooms/:id", roomController.UpdateRoom)
				timetable.DELETE("/rooms/:id", roomController.DeleteRoom)

				// Staff
				timetable.POST("/staff", staffController.CreateStaff)
				timetable.GET("/staff", staffController.GetAllStaff)
				timetable.GET("/staff/:id", staffController.GetStaff)
				timetable.PUT("/staff/:id", staffController.UpdateStaff)
				timetable.DELETE("/staff/:id", staffController.DeleteStaff)
				// Gin requires the same wildcard name as /staff/:id
				timetable.POST("/staff/:id/modules/:module_id", staffController.AssignModule)
				timetable.DELETE("/staff/:id/modules/:module_id", staffController.UnassignModule)
				timetable.GET("/staff/:id/modules", staffController.ListStaffModules)

				// Subjects
				timetable.POST("/subjects", subjectController.CreateSubject)
				timetable.GET("/subjects", subjectController.GetAllSubjects)
				timetable.GET("/subjects/:id", subjectController.GetSubject)
				timetable.PUT("/subjects/:id", subjectController.UpdateSubject)
				timetable.DELETE("/subjects/:id", subjectController.DeleteSubject)

				// Timetable (static paths before /:id)
				timetable.POST("/generate", timetableController.GenerateTimetable)
				timetable.POST("/generate/preview", timetableController.PreviewGenerateTimetable)
				timetable.GET("/class/:class_id", timetableController.GetTimetableByClass)
				timetable.GET("/by-staff/:staff_id", timetableController.GetTimetableByStaff)
				timetable.GET("/by-course/:course_id", timetableController.GetTimetableByCourse)
				timetable.GET("/validate", timetableController.ValidateTimetable)
				timetable.POST("/", timetableController.CreateTimetable)
				timetable.GET("/:id", timetableController.GetTimetable)
				timetable.PUT("/:id", timetableController.UpdateTimetable)
				timetable.DELETE("/:id", timetableController.DeleteTimetable)
			}
		}
	}
}
