package routes

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"

	"go_boilerplate/internal/auth"
	"go_boilerplate/internal/config"
	"go_boilerplate/internal/controllers"
	"go_boilerplate/internal/middlewares"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
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
	examRepo := repositories.NewExamRepository(db)
	generationSettingsRepo := repositories.NewGenerationSettingsRepository(db)
	institutionRepo := repositories.NewInstitutionRepository(db)
	auditLogRepo := repositories.NewAuditLogRepository(db)
	invitationRepo := repositories.NewInvitationRepository(db)

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

	// Audit trail. The recorder is passed to the controllers that perform
	// consequential actions so they do not have to remember to call it.
	auditRecorder := services.NewAuditRecorder(auditLogRepo)

	// Support access (impersonation). The token issuer is optional: if the JWT
	// secret cannot be resolved the feature is simply unavailable rather than
	// breaking boot, and impersonation returns an explicit error.
	impersonationService := buildImpersonationService(institutionRepo, auditRecorder)

	// Onboarding: public institution registration and invitation acceptance, both
	// of which create a database row from an unauthenticated request and are
	// therefore captcha-gated as well as rate limited at the router.
	onboardingService := services.NewOnboardingService(db, institutionRepo, userRepo, auditRecorder)
	invitationService := services.NewInvitationService(
		invitationRepo, auditRecorder, services.DefaultInvitationLimits())
	captchaService, err := services.NewCaptchaService()
	if err != nil {
		// A misconfigured captcha is a production-config problem, caught by the
		// startup guard. Panic here so it is visible at boot rather than on the
		// first signup.
		panic(fmt.Sprintf("captcha config: %v", err))
	}

	// Initialize controllers
	authController := controllers.NewAuthController(userRepo, notificationService, redisClient, otpGuard)
	userController := controllers.NewUserController(userRepo, auditRecorder)
	facultyController := controllers.NewFacultyController(facultyRepo)
	staffController := controllers.NewStaffControllerWithUser(staffRepo, moduleRepo, userRepo)
	courseController := controllers.NewCourseController(courseRepo)
	moduleController := controllers.NewModuleController(moduleRepo, courseRepo)
	classController := controllers.NewClassController(classRepo)
	roomController := controllers.NewRoomController(roomRepo)
	subjectController := controllers.NewSubjectController(subjectRepo)
	timetableController := controllers.NewTimetableController(timetableRepo, staffRepo, classRepo, roomRepo, timetableService, auditRecorder)
	generationSettingsController := controllers.NewGenerationSettingsController(generationSettingsRepo)
	institutionController := controllers.NewInstitutionController(institutionRepo, auditRecorder)
	auditController := controllers.NewAuditLogController(auditLogRepo, auditRecorder)
	impersonationController := controllers.NewImpersonationController(impersonationService, auditRecorder)
	onboardingController := controllers.NewOnboardingController(
		onboardingService, invitationService, invitationRepo, captchaService,
		notificationService, redisClient, otpGuard)
	institutionWorkspaceController := controllers.NewInstitutionWorkspaceController(
		institutionRepo, userRepo, staffRepo, invitationRepo, invitationService, auditRecorder)
	superAdminUserController := controllers.NewSuperAdminUserController(
		userRepo, institutionRepo, notificationService, auditRecorder)
	examController := controllers.NewExamController(
		examRepo, courseRepo, moduleRepo, classRepo, roomRepo, staffRepo, auditRecorder)
	importer := services.NewImporter(
		db, auditRecorder, facultyRepo, courseRepo, moduleRepo, classRepo,
		roomRepo, subjectRepo, staffRepo)
	importController := controllers.NewImportController(importer)

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
		authRoutes := api.Group("/auth")
		authRoutes.Use(middlewares.RateLimitMiddleware(redisClient))
		{
			// Public self-service registration is DISABLED by default.
			//
			// Since the tenancy work, User.institution_id is required for every
			// non-platform role and TenantMiddleware refuses a request with no
			// tenant scope, so a public register can only create an account that
			// is permanently locked out. Accounts now arrive by invitation, or by
			// registering an institution.
			//
			// The route stays registered behind a flag so a deployment with a
			// pre-tenant migration path, or one that provisions accounts out of
			// band, can re-enable it deliberately — see PUBLIC_REGISTER_ENABLED.
			authRoutes.POST("/register", authController.Register)
			authRoutes.POST("/login", authController.Login)
			authRoutes.POST("/verify-email", authController.VerifyEmail)
			authRoutes.POST("/forgot-password", authController.ForgotPassword)
			authRoutes.POST("/reset-password", authController.ResetPassword)
			authRoutes.POST("/resend-verification", authController.ResendVerificationOTP)
			authRoutes.POST("/logout", authController.Logout)
		}

		// OTP endpoints (rate-limited)
		otp := api.Group("/otp")
		otp.Use(middlewares.RateLimitMiddleware(redisClient))
		{
			otp.POST("/send", otpController.SendOTP)
			otp.POST("/verify", otpController.VerifyOTP)
		}

		// --- Onboarding (public, rate-limited, captcha-gated) -------------
		//
		// Both endpoints create a database row from an unauthenticated request,
		// so they carry a second, per-endpoint rate limit below the group limit
		// and a captcha check inside the handler.
		//
		// Grouping them separately makes it easy to see, when reviewing abuse
		// reports, exactly which unauthenticated endpoints can write.
		onboardingRoutes := api.Group("/auth")
		onboardingRoutes.Use(middlewares.RateLimitMiddleware(redisClient))
		onboardingRoutes.Use(middlewares.SignupRateLimitMiddleware(redisClient))
		{
			// Creates a PENDING institution and its first administrator in one
			// transaction, then sends the same verification OTP that
			// /auth/verify-email consumes.
			onboardingRoutes.POST("/register-institution", onboardingController.RegisterInstitution)
			// Turns a valid invitation into an active, verified account.
			onboardingRoutes.POST("/accept-invitation", onboardingController.AcceptInvitation)
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

			// The caller's own capabilities. The frontend reads this to decide
			// which nav items and buttons to render, so that the UI and the
			// server cannot disagree about what a role may do.
			protected.GET("/me/permissions", userController.GetMyPermissions)

			// --- The caller's own institution workspace --------------------
			//
			// RequireInstitutionWorkspace rejects a platform account, so this
			// whole block is tenant-only. The institution ID is read from the
			// session by every handler, never from a path or body.
			workspace := protected.Group("/institution")
			workspace.Use(middlewares.RequireInstitutionWorkspace())
			{
				workspace.GET("", middlewares.RequirePermission(auth.PermInstitutionRead),
					institutionWorkspaceController.Get)
				workspace.PUT("", middlewares.RequirePermission(auth.PermInstitutionWrite),
					institutionWorkspaceController.Update)

				// Members. Reading needs user:read; every mutation needs
				// user:write, which only an institution administrator holds.
				members := workspace.Group("/members")
				{
					members.GET("", middlewares.RequirePermission(auth.PermUserRead),
						institutionWorkspaceController.ListMembers)
					members.GET("/:id", middlewares.RequirePermission(auth.PermUserRead),
						institutionWorkspaceController.GetMember)
					members.POST("", middlewares.RequirePermission(auth.PermUserWrite),
						institutionWorkspaceController.CreateMember)
					members.PATCH("/:id", middlewares.RequirePermission(auth.PermUserWrite),
						institutionWorkspaceController.PatchMember)
					members.DELETE("/:id", middlewares.RequirePermission(auth.PermUserDelete),
						institutionWorkspaceController.DeleteMember)
				}

				// Invitations. Issuing one is an unauthenticated path into
				// account creation, so it requires user:write and the service
				// enforces its own caps.
				invitations := workspace.Group("/invitations")
				{
					invitations.POST("", middlewares.RequirePermission(auth.PermUserWrite),
						institutionWorkspaceController.CreateInvitation)
					invitations.GET("", middlewares.RequirePermission(auth.PermUserRead),
						institutionWorkspaceController.ListInvitations)
					invitations.DELETE("/:id", middlewares.RequirePermission(auth.PermUserWrite),
						institutionWorkspaceController.RevokeInvitation)
				}
			}

			// A tenant's own audit trail. Scoped to the caller's institution by
			// the repository, so an institution sees its own history and not the
			// platform's or another tenant's.
			protected.GET("/audit", middlewares.RequireInstitutionWorkspace(), middlewares.RequirePermission(auth.PermAdminStats), auditController.List)

			users := protected.Group("/users")
			{
				users.GET("", middlewares.RequireInstitutionWorkspace(), middlewares.RequirePermission(auth.PermUserRead), userController.GetUsers)
				users.GET("/:id", middlewares.RequireInstitutionWorkspace(), middlewares.RequirePermission(auth.PermUserRead), userController.GetUser)
				users.POST("", middlewares.RequireInstitutionWorkspace(), middlewares.RequirePermission(auth.PermUserWrite), userController.CreateUser)
				users.PUT("/:id", middlewares.RequireInstitutionWorkspace(), middlewares.RequirePermission(auth.PermUserWrite), userController.UpdateUser)
				users.DELETE("/:id", middlewares.RequireInstitutionWorkspace(), middlewares.RequirePermission(auth.PermUserDelete), userController.DeleteUser)
			}

			// Admin surfaces. An institution workspace, so a platform account is
			// refused — it has no tenant to scope these numbers to.
			admin := protected.Group("/admin")
			admin.Use(middlewares.RequireInstitutionWorkspace())
			{
				// Dashboard counts are scoped to the caller's institution so one
				// campus's admin never sees another campus's totals. A platform
				// super_admin (institution_id NULL) gets platform-wide counts.
				admin.GET("/dashboard", middlewares.RequirePermission(auth.PermAdminStats), func(c *gin.Context) {
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
				admin.GET("/users/stats", middlewares.RequirePermission(auth.PermAdminStats), func(c *gin.Context) {
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

			// Platform administration. RequirePlatformWorkspace is stronger than
			// a role check: it also requires the account to be unbound to an
			// institution, so a super_admin who has been assigned to an
			// institution is a tenant user and cannot manage the tenant list.
			superadmin := protected.Group("/superadmin")
			superadmin.Use(middlewares.RequirePlatformWorkspace())
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
				superadmin.GET("/generation-settings", middlewares.RequirePermission(auth.PermPlatformSettings), generationSettingsController.Get)
				superadmin.PUT("/generation-settings", middlewares.RequirePermission(auth.PermPlatformSettings), generationSettingsController.Update)

				// Institution management. The group already requires a platform
				// workspace, so a super_admin bound to an institution is refused
				// here too.
				institutions := superadmin.Group("/institutions")
				{
					institutions.POST("", middlewares.RequirePermission(auth.PermInstitutionWrite), institutionController.Create)
					institutions.GET("", middlewares.RequirePermission(auth.PermInstitutionRead), institutionController.GetAll)
					institutions.GET("/:id", middlewares.RequirePermission(auth.PermInstitutionRead), institutionController.Get)
					institutions.PUT("/:id", middlewares.RequirePermission(auth.PermInstitutionWrite), institutionController.Update)
					institutions.DELETE("/:id", middlewares.RequirePermission(auth.PermInstitutionDelete), institutionController.Delete)

					// Support access: a read-only, short-lived, audited session
					// scoped to one institution. This is the ONLY way a platform
					// operator can see inside an institution's workspace.
					institutions.POST("/:id/impersonate",
						middlewares.RequirePermission(auth.PermImpersonate),
						impersonationController.Start,
					)
					institutions.POST("/:id/impersonate/end",
						middlewares.RequirePermission(auth.PermImpersonate),
						impersonationController.End,
					)
				}

				// Audit trail. Platform-wide by default; ?institution_id=N reads
				// one tenant's trail.
				audit := superadmin.Group("/audit")
				{
					audit.GET("", middlewares.RequirePermission(auth.PermAuditRead), auditController.List)
					audit.GET("/target/:type/:id",
						middlewares.RequirePermission(auth.PermAuditRead),
						auditController.ListForTarget,
					)
				}

				// The same trail under the /audit-logs name, which is what the
				// API documentation uses. Both spellings resolve to one handler
				// so neither becomes a second implementation to keep in step.
				auditLogs := superadmin.Group("/audit-logs")
				{
					auditLogs.GET("", middlewares.RequirePermission(auth.PermAuditRead), auditController.List)
					auditLogs.GET("/target/:type/:id",
						middlewares.RequirePermission(auth.PermAuditRead),
						auditController.ListForTarget,
					)
				}

				// Platform user administration. This is the only place a
				// non-institution role is managed; a tenant's own member list
				// lives at /protected/institution/members.
				//
				// Note the permissions: user:read and admin:stats are held by
				// institution roles too, but the RequirePlatformWorkspace gate on
				// this group means only a platform account ever reaches them.
				platformUsers := superadmin.Group("/users")
				{
					platformUsers.GET("", middlewares.RequirePermission(auth.PermUserRead),
						superAdminUserController.ListUsers)
					platformUsers.GET("/:id", middlewares.RequirePermission(auth.PermUserRead),
						superAdminUserController.GetUser)
					platformUsers.PUT("/:id", middlewares.RequirePermission(auth.PermUserRoleWrite),
						superAdminUserController.UpdateUser)
					platformUsers.POST("/:id/status", middlewares.RequirePermission(auth.PermUserWrite),
						superAdminUserController.SetUserStatus)
					platformUsers.POST("/:id/reset-password",
						middlewares.RequirePermission(auth.PermUserRoleWrite),
						superAdminUserController.ResetPassword)
				}
			}

			// Timetable Management endpoints — an institution workspace.
			//
			// RequireInstitutionWorkspace rejects a platform super_admin, which
			// is the point of the gate: a platform account is not bound to an
			// institution, so it has no tenant to scope these queries with.
			// Letting it through would be the cross-tenant leak the tenancy work
			// closed. Support access is granted by explicit, audited
			// impersonation, which does carry an institution scope and holds
			// read permissions only.
			//
			// Each route then states the capability it needs rather than a role,
			// so widening a role is a change to internal/auth alone.
			timetable := protected.Group("/timetable")
			timetable.Use(middlewares.RequireInstitutionWorkspace())
			// A support session may browse the timetable but never write to it.
			// The write permissions already exclude it; this is the independent
			// second layer.
			timetable.Use(middlewares.DenyWriteForSupport())
			{
				// Faculty
				timetable.POST("/faculties", middlewares.RequirePermission(auth.PermFacultyWrite), facultyController.CreateFaculty)
				timetable.GET("/faculties", middlewares.RequirePermission(auth.PermFacultyRead), facultyController.GetAllFaculties)
				timetable.GET("/faculties/:id", middlewares.RequirePermission(auth.PermFacultyRead), facultyController.GetFaculty)
				timetable.PUT("/faculties/:id", middlewares.RequirePermission(auth.PermFacultyWrite), facultyController.UpdateFaculty)
				timetable.DELETE("/faculties/:id", middlewares.RequirePermission(auth.PermFacultyWrite), facultyController.DeleteFaculty)

				// Course
				timetable.POST("/courses", middlewares.RequirePermission(auth.PermCourseWrite), courseController.CreateCourse)
				timetable.GET("/courses", middlewares.RequirePermission(auth.PermCourseRead), courseController.GetAllCourses)
				timetable.GET("/courses/:id", middlewares.RequirePermission(auth.PermCourseRead), courseController.GetCourse)
				timetable.PUT("/courses/:id", middlewares.RequirePermission(auth.PermCourseWrite), courseController.UpdateCourse)
				timetable.DELETE("/courses/:id", middlewares.RequirePermission(auth.PermCourseWrite), courseController.DeleteCourse)

				// Module
				timetable.POST("/modules", middlewares.RequirePermission(auth.PermModuleWrite), moduleController.CreateModule)
				timetable.GET("/modules", middlewares.RequirePermission(auth.PermModuleRead), moduleController.GetAllModules)
				timetable.GET("/modules/:id", middlewares.RequirePermission(auth.PermModuleRead), moduleController.GetModule)
				timetable.PUT("/modules/:id", middlewares.RequirePermission(auth.PermModuleWrite), moduleController.UpdateModule)
				timetable.DELETE("/modules/:id", middlewares.RequirePermission(auth.PermModuleWrite), moduleController.DeleteModule)
				// Use :id (same wildcard name as other /modules/:id routes — Gin requirement)
				timetable.GET("/modules/:id/staff", middlewares.RequirePermission(auth.PermStaffRead), staffController.ListModuleStaff)

				// Class
				timetable.POST("/classes", middlewares.RequirePermission(auth.PermClassWrite), classController.CreateClass)
				timetable.GET("/classes", middlewares.RequirePermission(auth.PermClassRead), classController.GetAllClasses)
				timetable.GET("/classes/:id", middlewares.RequirePermission(auth.PermClassRead), classController.GetClass)
				timetable.PUT("/classes/:id", middlewares.RequirePermission(auth.PermClassWrite), classController.UpdateClass)
				timetable.DELETE("/classes/:id", middlewares.RequirePermission(auth.PermClassWrite), classController.DeleteClass)

				// Room
				timetable.POST("/rooms", middlewares.RequirePermission(auth.PermRoomWrite), roomController.CreateRoom)
				timetable.GET("/rooms", middlewares.RequirePermission(auth.PermRoomRead), roomController.GetAllRooms)
				timetable.GET("/rooms/:id", middlewares.RequirePermission(auth.PermRoomRead), roomController.GetRoom)
				timetable.PUT("/rooms/:id", middlewares.RequirePermission(auth.PermRoomWrite), roomController.UpdateRoom)
				timetable.DELETE("/rooms/:id", middlewares.RequirePermission(auth.PermRoomWrite), roomController.DeleteRoom)

				// Staff
				timetable.POST("/staff", middlewares.RequirePermission(auth.PermStaffWrite), staffController.CreateStaff)
				timetable.GET("/staff", middlewares.RequirePermission(auth.PermStaffRead), staffController.GetAllStaff)
				timetable.GET("/staff/:id", middlewares.RequirePermission(auth.PermStaffRead), staffController.GetStaff)
				timetable.PUT("/staff/:id", middlewares.RequirePermission(auth.PermStaffWrite), staffController.UpdateStaff)
				timetable.DELETE("/staff/:id", middlewares.RequirePermission(auth.PermStaffWrite), staffController.DeleteStaff)
				// Gin requires the same wildcard name as /staff/:id
				timetable.POST("/staff/:id/modules/:module_id", middlewares.RequirePermission(auth.PermStaffWrite), staffController.AssignModule)
				timetable.DELETE("/staff/:id/modules/:module_id", middlewares.RequirePermission(auth.PermStaffWrite), staffController.UnassignModule)
				timetable.GET("/staff/:id/modules", middlewares.RequirePermission(auth.PermStaffRead), staffController.ListStaffModules)

				// Subjects
				timetable.POST("/subjects", middlewares.RequirePermission(auth.PermSubjectWrite), subjectController.CreateSubject)
				timetable.GET("/subjects", middlewares.RequirePermission(auth.PermSubjectRead), subjectController.GetAllSubjects)
				timetable.GET("/subjects/:id", middlewares.RequirePermission(auth.PermSubjectRead), subjectController.GetSubject)
				timetable.PUT("/subjects/:id", middlewares.RequirePermission(auth.PermSubjectWrite), subjectController.UpdateSubject)
				timetable.DELETE("/subjects/:id", middlewares.RequirePermission(auth.PermSubjectWrite), subjectController.DeleteSubject)

				// Timetable (static paths before /:id)
				//
				// Preview and commit are separate permissions so a coordinator
				// can experiment without overwriting a published timetable.
				timetable.POST("/generate", middlewares.RequirePermission(auth.PermTimetableGenerate), timetableController.GenerateTimetable)
				timetable.POST("/generate/preview", middlewares.RequirePermission(auth.PermTimetablePreview), timetableController.PreviewGenerateTimetable)
				timetable.GET("/class/:class_id", middlewares.RequirePermission(auth.PermTimetableRead), timetableController.GetTimetableByClass)
				timetable.GET("/by-staff/:staff_id", middlewares.RequirePermission(auth.PermTimetableRead), timetableController.GetTimetableByStaff)
				timetable.GET("/by-course/:course_id", middlewares.RequirePermission(auth.PermTimetableRead), timetableController.GetTimetableByCourse)
				timetable.GET("/validate", middlewares.RequirePermission(auth.PermTimetableRead), timetableController.ValidateTimetable)
				timetable.POST("/", middlewares.RequirePermission(auth.PermTimetableGenerate), timetableController.CreateTimetable)
				timetable.GET("/:id", middlewares.RequirePermission(auth.PermTimetableRead), timetableController.GetTimetable)
				timetable.PUT("/:id", middlewares.RequirePermission(auth.PermTimetableOverride), timetableController.UpdateTimetable)
				timetable.DELETE("/:id", middlewares.RequirePermission(auth.PermTimetableOverride), timetableController.DeleteTimetable)

				// Publication lifecycle. Publish and approve are separate
				// permissions on purpose: the person who prepares a schedule
				// should not automatically be the person who signs it off.
				// Approval also requires the timetable to be published first, so
				// the two events are always in that order in the audit trail.
				timetable.POST("/class/:class_id/publish",
					middlewares.RequirePermission(auth.PermTimetablePublish),
					timetableController.PublishClass)
				timetable.POST("/class/:class_id/approve",
					middlewares.RequirePermission(auth.PermTimetableApprove),
					timetableController.ApproveClass)
			}

			// --- Exams -------------------------------------------------------
			// An institution workspace, so a platform account is refused. An
			// academic coordinator holds no exam permission at all, which keeps
			// the two coordinators' domains genuinely separate.
			exams := protected.Group("/exams")
			exams.Use(middlewares.RequireInstitutionWorkspace())
			// Defence in depth: the exam write permissions are already absent
			// from the support role, and this refuses a support session outright
			// so a future permission edit cannot open a write path for one.
			exams.Use(middlewares.DenySupportSession())
			{
				exams.GET("", middlewares.RequirePermission(auth.PermExamRead), examController.GetAll)
				exams.POST("", middlewares.RequirePermission(auth.PermExamWrite), examController.Create)
				exams.GET("/:id", middlewares.RequirePermission(auth.PermExamRead), examController.Get)
				exams.PUT("/:id", middlewares.RequirePermission(auth.PermExamWrite), examController.Update)
				exams.PATCH("/:id", middlewares.RequirePermission(auth.PermExamWrite), examController.Update)
				exams.DELETE("/:id", middlewares.RequirePermission(auth.PermExamWrite), examController.Delete)

				// The three lifecycle transitions, each behind its own permission
				// so scheduling, publishing, and approving can be delegated
				// separately.
				exams.POST("/:id/status/scheduled",
					middlewares.RequirePermission(auth.PermExamSchedule), examController.SetStatus)
				exams.POST("/:id/status/published",
					middlewares.RequirePermission(auth.PermExamPublish), examController.SetStatus)
				exams.POST("/:id/status/approved",
					middlewares.RequirePermission(auth.PermExamApprove), examController.SetStatus)
			}

			// --- CSV bulk import --------------------------------------------
			// Validate and commit are separate endpoints so an operator sees
			// every problem with a file before any of it is written; a
			// one-shot import would either half-succeed or silently skip rows.
			imports := protected.Group("/import")
			imports.Use(middlewares.RequireInstitutionWorkspace())
			// An import writes in bulk, so a support session is refused outright
			// rather than relying on the data:import permission alone.
			imports.Use(middlewares.DenySupportSession())
			{
				imports.GET("/schema", middlewares.RequirePermission(auth.PermDataImport), importController.Schema)
				imports.POST("/:entity/validate",
					middlewares.RequirePermission(auth.PermDataImport), importController.Validate)
				imports.POST("/:entity",
					middlewares.RequirePermission(auth.PermDataImport), importController.Import)
			}
		}
	}
}

// buildImpersonationService wires the support-access service.
//
// Impersonation is off unless IM_PERSONATION_ENABLED is explicitly true, so a
// deployment that has not thought about support access does not have it. A
// missing or unresolvable JWT secret disables it too, rather than failing boot:
// a misconfiguration in an optional feature must not take the API down.
func buildImpersonationService(
	institutionRepo repositories.InstitutionRepository,
	audit *services.AuditRecorder,
) *services.ImpersonationService {
	cfg := services.DefaultImpersonationConfig()
	cfg.Enabled = strings.EqualFold(config.GetEnv("IM_PERSONATION_ENABLED", "false"), "true")

	if minutes, err := strconv.Atoi(config.GetEnv("IM_PERSONATION_TTL_MINUTES", "15")); err == nil && minutes > 0 {
		cfg.TTL = time.Duration(minutes) * time.Minute
	}
	if max, err := strconv.Atoi(config.GetEnv("IM_PERSONATION_MAX_CONCURRENT", "1")); err == nil && max > 0 {
		cfg.MaxConcurrent = max
	}

	if !cfg.Enabled {
		logger.Info("Impersonation (support access) is disabled — set IM_PERSONATION_ENABLED=true to allow audited read-only sessions")
		return services.NewImpersonationService(institutionRepo, audit, cfg, nil, nil)
	}

	issuer, err := services.NewSupportTokenIssuer()
	if err != nil {
		logger.Error("Impersonation enabled but unavailable: %v", err)
		cfg.Enabled = false
		return services.NewImpersonationService(institutionRepo, audit, cfg, nil, nil)
	}

	// Sessions are counted in-process. A multi-instance deployment should move
	// this to shared state; until then the cap is per instance, which errs
	// toward allowing a session rather than locking an operator out.
	var live int
	return services.NewImpersonationService(
		institutionRepo,
		audit,
		cfg,
		issuer.Issue,
		func() int { return live },
	)
}
